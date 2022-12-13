package api

import (
	"fmt"
	"strings"

	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/golang/protobuf/proto"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/smartbch/ganychain/backend"
	pb "github.com/smartbch/ganychain/proto"
)

var _ PublicGanyAPI = (*ganyAPI)(nil)

type PublicGanyAPI interface {
	ChainIds() []string
	PutBulletin(tx hexutil.Bytes) (tmbytes.HexBytes, error)
	GetBulletin(ganyUrl string) (hexutil.Bytes, error)
	QueryBulletins(typ pb.Bulletin_BulletinType, topicHash hexutil.Bytes, start, end int64, snListBz []hexutil.Bytes) ([]hexutil.Bytes, error)
	GetDelegatedAddr(mainAddr gethcmn.Address) (gethcmn.Address, error)
	LoadWalletInStochasticPay(tokenAddr, ownerAddr gethcmn.Address) ([]hexutil.Bytes, error)
	GetValidatorPubKeyList() ([]hexutil.Bytes, error)
}

type ganyAPI struct {
	backend backend.BackendService
	logger  tmlog.Logger
}

func newGanyAPI(backend backend.BackendService, logger tmlog.Logger) PublicGanyAPI {
	return &ganyAPI{
		backend: backend,
		logger:  logger,
	}
}

func (g *ganyAPI) ChainIds() []string {
	g.logger.Debug("gany_chainIds")
	return g.backend.GetAllChainIds()
}

func (g *ganyAPI) PutBulletin(tx hexutil.Bytes) (tmbytes.HexBytes, error) {
	g.logger.Debug("gany_putBulletin")

	hash, err := g.backend.PutBulletin(pb.GanyTx(tx))
	if err != nil {
		return nil, err
	}
	return hash, nil
}

func (g *ganyAPI) GetBulletin(ganyUrl string) (hexutil.Bytes, error) {
	g.logger.Debug("gany_getBulletin")

	prefix := "gany://"
	// Gany URL: gany://TopicHash4hex.BlockTime5decimal.TxIndex3decimal (hex string without 0x)
	if !strings.HasPrefix(ganyUrl, prefix) {
		return nil, fmt.Errorf("invaliad gany url prefix")
	}

	urlHexStr := "0x" + strings.TrimPrefix(ganyUrl, prefix)
	ganyUrlBz, err := hexutil.Decode(urlHexStr)
	if err != nil {
		return nil, err
	}

	fmt.Printf("ganyUrlBz: %v\n", ganyUrlBz)

	b, err := g.backend.GetBulletinByGanyUrl(ganyUrlBz)
	if err != nil {
		return nil, err
	}

	bz, err := proto.Marshal(b)
	if err != nil {
		return nil, err
	}

	return bz, nil
}

func (g *ganyAPI) QueryBulletins(typ pb.Bulletin_BulletinType, topicHash hexutil.Bytes, start, end int64, snListBz []hexutil.Bytes) ([]hexutil.Bytes, error) {
	g.logger.Debug("gany_queryBulletins")

	if len(topicHash) != 32 {
		return nil, fmt.Errorf("topic hash length %d != 32", len(topicHash))
	}

	excludeSNs := make(map[string]struct{})
	for i := 0; i < len(snListBz); i++ {
		excludeSNs[hexutil.Encode(snListBz[i])] = struct{}{}
	}

	var topicHashBz32 [32]byte
	copy(topicHashBz32[:], topicHash[:])
	bs, err := g.backend.QueryBulletinByTimePeriod(typ, topicHashBz32, start, end, excludeSNs)
	if err != nil {
		return nil, err
	}
	fmt.Printf("query bulletins: %+v\n", bs)

	results := make([]hexutil.Bytes, 0, len(bs))
	for _, b := range bs {
		bz, err := proto.Marshal(b)
		if err != nil {
			return nil, err
		}
		results = append(results, bz)
	}
	fmt.Printf("query results: %v\n", results)

	return results, nil
}

// -----------------------------Only for test-----------------------------------------------

func (g *ganyAPI) GetDelegatedAddr(mainAddr gethcmn.Address) (gethcmn.Address, error) {
	g.logger.Debug("gany_getDelegatedAddr")
	return g.backend.GetDelegatedAddr(mainAddr)
}

func (g *ganyAPI) LoadWalletInStochasticPay(tokenAddr, ownerAddr gethcmn.Address) ([]hexutil.Bytes, error) {
	g.logger.Debug("gany_loadWalletInStochasticPay")

	nonces, balance, err := g.backend.LoadWalletInStochasticPay(tokenAddr, ownerAddr)
	if err != nil {
		return nil, err
	}

	noncesBz := nonces.Bytes32()
	balanceBz := balance.Bytes32()
	fmt.Printf("nonces: %v\n", noncesBz)
	fmt.Printf("balance: %v\n", balanceBz)

	return []hexutil.Bytes{
		noncesBz[:], balanceBz[:],
	}, nil
}

func (g *ganyAPI) GetValidatorPubKeyList() ([]hexutil.Bytes, error) {
	g.logger.Debug("gany_getValidatorPubKeyList")
	pubKeyList := g.backend.GetValidatorPubKeyList()

	results := make([]hexutil.Bytes, 0, len(pubKeyList))
	for _, pubKey := range pubKeyList {
		results = append(results, pubKey)
	}

	return results, nil
}
