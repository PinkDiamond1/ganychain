package follower

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path"
	"sync"
	"time"

	geth "github.com/ethereum/go-ethereum"
	gethcmn "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
	"github.com/smartbch/moeingads"
	"github.com/smartbch/moeingads/store"
	"github.com/smartbch/moeingads/store/rabbit"
	"github.com/smartbch/moeingdb/modb"
	modbtypes "github.com/smartbch/moeingdb/types"
	moevmtypes "github.com/smartbch/moeingevm/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tmtypes "github.com/tendermint/tendermint/types"
	"go.uber.org/atomic"

	"github.com/smartbch/ganychain/contract"
	"github.com/smartbch/ganychain/web3client"
)

const (
	SBCHChainId = 10001

	// event ValidatorsElect(uint indexed electedTime);
	EventGanyGovElectValidatorTopic0 = "0xd246c04483b27b20b1e0306e279de895a4a048e1e831d07f4da5e4c9253459d3"
)

type SbchFollower struct {
	// logger
	logger tmlog.Logger

	// follower
	followerConfig *ChainConfig
	sbchChainId    *uint256.Int

	mads         *moeingads.MoeingADS
	root         *store.RootStore
	trunk        *store.TrunkStore
	historyStore modbtypes.DB

	sbchHeight int64
	block      *moevmtypes.Block
	blockInfo  atomic.Value // to store *moevmtypes.BlockInfo
	sbchClient web3client.Web3Client

	// cached status
	mu                  sync.RWMutex // guards cached status
	validatorPubKeyList [][]byte
}

func NewSbchFollower(followerConfig *ChainConfig, logger tmlog.Logger, sbchClient web3client.Web3Client) FollowerService {
	app := &SbchFollower{}
	app.logger = logger
	app.followerConfig = followerConfig
	app.sbchChainId = uint256.NewInt(SBCHChainId)

	app.root, app.mads = CreateRootStore(app.followerConfig)
	app.historyStore = CreateHistoryStore(app.followerConfig, app.logger.With("module", "modb"))
	app.sbchHeight = app.historyStore.GetLatestHeight()
	app.logger.Info("storeHeight", app.sbchHeight)
	if app.sbchHeight == 0 {
		app.initGenesisState()
	}

	app.sbchClient = sbchClient

	// init the cache data
	app.validatorPubKeyList = make([][]byte, 0, 4)

	// start follower
	go app.runFollower(app.sbchHeight)
	go app.runLogCatcher(followerConfig.SmartBchWsUrl)
	return app
}

// ---------------------------------Follower------------------------------------------

func CreateRootStore(config *ChainConfig) (*store.RootStore, *moeingads.MoeingADS) {
	first := [8]byte{0, 0, 0, 0, 0, 0, 0, 0}
	last := [8]byte{255, 255, 255, 255, 255, 255, 255, 255}
	mads, err := moeingads.NewMoeingADS(config.AppConfig.AppDataPath, config.AppConfig.ArchiveMode,
		[][]byte{first[:], last[:]})
	if err != nil {
		panic(err)
	}
	root := store.NewRootStore(mads, func(k []byte) bool {
		return len(k) >= 1 && k[0] > (128+64) //only cache the standby queue
	})
	return root, mads
}

func CreateHistoryStore(config *ChainConfig, logger tmlog.Logger) (historyStore modbtypes.DB) {
	modbDir := config.AppConfig.ModbDataPath
	if config.AppConfig.UseLiteDB {
		historyStore = modb.NewLiteDB(modbDir)
	} else {
		if _, err := os.Stat(modbDir); os.IsNotExist(err) {
			_ = os.MkdirAll(path.Join(modbDir, "data"), 0700)
			var seed [8]byte // use current time as moeingdb's hash seed
			binary.LittleEndian.PutUint64(seed[:], uint64(time.Now().UnixNano()))
			historyStore = modb.CreateEmptyMoDB(modbDir, seed, logger)
		} else {
			historyStore = modb.NewMoDB(modbDir, logger)
		}
		historyStore.SetMaxEntryCount(config.AppConfig.RpcEthGetLogsMaxResults)
	}
	return
}

func (app *SbchFollower) initGenesisState() {
	genFile := app.followerConfig.AppConfig.GenesisFilePath
	genDoc := &tmtypes.GenesisDoc{}
	if _, err := os.Stat(genFile); err != nil {
		if !os.IsNotExist(err) {
			panic(err)
		}
	} else {
		genDoc, err = tmtypes.GenesisDocFromFile(genFile)
		if err != nil {
			panic(err)
		}
	}

	var genesisData struct {
		Alloc gethcore.GenesisAlloc `json:"alloc"`
	}
	err := json.Unmarshal(genDoc.AppState, &genesisData)
	if err != nil {
		panic(err)
	}
	app.logger.Info("initGenesisState", "genesis data", string(genDoc.AppState))
	app.trunk = app.root.GetTrunkStore(app.followerConfig.AppConfig.TrunkCacheSize).(*store.TrunkStore)
	app.root.SetHeight(0)
	app.createGenesisAccounts(genesisData.Alloc)
	app.trunk.Close(true)
}

func (app *SbchFollower) createGenesisAccounts(alloc gethcore.GenesisAlloc) {
	if len(alloc) == 0 {
		return
	}

	rbt := rabbit.NewRabbitStore(app.trunk)
	app.logger.Info("Initial air drop", "accounts", len(alloc))
	for addr, acc := range alloc {
		amt, _ := uint256.FromBig(acc.Balance)
		k := moevmtypes.GetAccountKey(addr)
		v := moevmtypes.ZeroAccountInfo()
		v.UpdateBalance(amt)
		rbt.Set(k, v.Bytes())
	}

	rbt.Close()
	rbt.WriteBack()
}

func (app *SbchFollower) runFollower(storeHeight int64) {
	app.logger.Info("Run follower", "storeHeight", storeHeight)
	// 1. fetch blocks until catch up leader.
	latestHeight := app.catchupLeader(storeHeight)
	// Run 2 times to catch blocks mint amount 1st catchupLeader running.
	latestHeight = app.catchupLeader(latestHeight)
	// 2. keep sync with leader.
	for {
		latestHeight = app.updateState(latestHeight + 1)
		time.Sleep(100 * time.Millisecond)
	}
}

func (app *SbchFollower) updateState(height int64) int64 {
	blk, err := app.sbchClient.GetSyncBlock(uint64(height))
	if err != nil {
		app.logger.Debug("updateState failed", "wantedHeight", height, "error", err)
		return height - 1
	}
	app.root.SetHeight(height)
	store.SyncUpdateTo(blk.UpdateOfADS, app.root)
	app.historyStore.AddBlock(&blk.Block, -1, blk.Txid2sigMap)
	app.sbchHeight = blk.Height
	app.block = &moevmtypes.Block{}
	_, err = app.block.UnmarshalMsg(blk.BlockInfo)
	if err != nil {
		panic(err)
	}
	app.syncBlockInfo()
	app.logger.Info("updateState done", "latestHeight", height)
	return height
}

func (app *SbchFollower) catchupLeader(storeHeight int64) int64 {
	latestHeight, err := app.sbchClient.GeLatestBlockHeight()
	for err != nil {
		app.logger.Error("catchupLeader failed", "error:", err)
		time.Sleep(3 * time.Second)
		latestHeight, err = app.sbchClient.GeLatestBlockHeight()
	}
	app.logger.Info("catchupLeader", "latestHeight", latestHeight)
	for h := storeHeight + 1; h <= latestHeight; h++ {
		h = app.updateState(h)
	}
	return latestHeight
}

func (app *SbchFollower) syncBlockInfo() *moevmtypes.BlockInfo {
	bi := &moevmtypes.BlockInfo{
		Coinbase:  app.block.Miner,
		Number:    app.block.Number,
		Timestamp: app.block.Timestamp,
		ChainId:   app.sbchChainId.Bytes32(),
		Hash:      app.block.Hash,
	}
	app.blockInfo.Store(bi)
	app.logger.Info("blockInfo", "height", bi.Number, "hash", gethcmn.Hash(bi.Hash).Hex())
	return bi
}

func (app *SbchFollower) getRpcContext() *moevmtypes.Context {
	c := moevmtypes.NewContext(nil, nil)
	r := rabbit.NewReadOnlyRabbitStore(app.root)
	c = c.WithRbt(&r)
	c = c.WithDb(app.historyStore)
	c.SetCurrentHeight(app.sbchHeight)
	return c
}

func (app *SbchFollower) runLogCatcher(wsUrl string) {
	// 1. start the websocket to listen to the logs
	// 2. once the election log is identified, read the validator list and cache it
	app.logger.Info("Run log catcher")

	wsClient, err := ethclient.Dial(wsUrl)
	if err != nil {
		panic(err)
	}

	logs := make(chan gethtypes.Log)
	sub, err := wsClient.SubscribeFilterLogs(context.Background(), geth.FilterQuery{
		Addresses: []gethcmn.Address{contract.GanyGovAddress},
	}, logs)
	if err != nil {
		panic(err)
	}

	for {
		select {
		case _ = <-sub.Err():
			continue
		case newLog := <-logs:
			if len(newLog.Topics) == 0 {
				continue
			}

			if newLog.Topics[0].Hex() == EventGanyGovElectValidatorTopic0 {
				// update validator list
				app.getValidatorPubKeyList()
			}
		}
	}
}

// ---------------------------------For Backend------------------------------------------

func (app *SbchFollower) GetDelegatedAddrByMainAddr(mainAddr gethcmn.Address) (gethcmn.Address, error) {
	return app.getDelegatedAddrByMainAddr(mainAddr)
}

func (app *SbchFollower) getDelegatedAddrByMainAddr(mainAddr gethcmn.Address) (gethcmn.Address, error) {
	ctx := app.getRpcContext()
	defer ctx.Close(false)
	return getDelegatedAddrByMainAddr(ctx, mainAddr)
}

func (app *SbchFollower) LoadWalletInStochasticPay(tokenAddress, ownerAddress gethcmn.Address) (*uint256.Int, *uint256.Int, error) {
	return app.loadWalletInStochasticPay(tokenAddress, ownerAddress)
}

func (app *SbchFollower) loadWalletInStochasticPay(tokenAddress, ownerAddress gethcmn.Address) (*uint256.Int, *uint256.Int, error) {
	ctx := app.getRpcContext()
	defer ctx.Close(false)
	return loadWalletInStochasticPay(ctx, tokenAddress, ownerAddress)
}

func (app *SbchFollower) GetValidatorPubKeyList() [][]byte {
	// 1. if found in cache, return the cache list
	if len(app.validatorPubKeyList) > 0 {
		app.mu.RLock()
		defer app.mu.RUnlock()
		return app.validatorPubKeyList
	}

	// 2. if not in cache, query the store
	return app.getValidatorPubKeyList()
}

func (app *SbchFollower) getValidatorPubKeyList() [][]byte {
	ctx := app.getRpcContext()
	defer ctx.Close(false)

	app.mu.Lock()
	defer app.mu.Unlock()
	app.validatorPubKeyList = getValidatorPubKeyList(ctx)
	return app.validatorPubKeyList
}
