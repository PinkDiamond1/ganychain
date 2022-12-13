package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/cespare/xxhash"
	"github.com/dgraph-io/badger/v3"
	"github.com/ethereum/go-ethereum/common/hexutil"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tmhttp "github.com/tendermint/tendermint/rpc/client/http"
	tmcoretypes "github.com/tendermint/tendermint/rpc/coretypes"

	pb "github.com/smartbch/ganychain/proto"
)

const (
	MaxTTL = 180 * 24 * time.Hour
	MinTTL = 1 * time.Hour

	MainKeyLen     = 1 + 8 + 5 + 8 + 8
	MainKeyHeadLen = 1 + 8 + 5

	TopicHashEnd    = 32
	HistoryCountEnd = 36

	TopicHashLen  = 32
	HisCountLen   = 4
	BulletinIdLen = 64

	MaxTimeDiff = int64(180)

	DefaultResultLength  = 8192
	DefaultValueLength   = 1024
	MaxQueryResultLength = 1024 * 1024
	MaxQueryResultCount  = 255

	MainKeyHeadByte = byte(220)

	CheckTxCodeOK                            = uint32(000)
	CheckTxCodeErrorInvalidTxBytes           = uint32(001)
	CheckTxCodeErrorInvalidBulletin          = uint32(002)
	CheckTxCodeErrorInvalidStochasticPayment = uint32(003)
	CheckTxCodeError                         = uint32(99)

	DeliverTxCodeErrorTimestampTooLong        = uint32(100)
	DeliverTxCodeErrorInvalidOldSN            = uint32(101)
	DeliverTxCodeErrorCannotFindOldBulletin   = uint32(102)
	DeliverTxCodeErrorCannotOverwriteBulletin = uint32(103)
	DeliverTxCodeErrorOther                   = uint32(104)
)

type GanyApp interface {
	// ABCI interface
	abcitypes.Application
	// Tendermint Client
	BroadcastTx(txBz []byte) (*tmcoretypes.ResultBroadcastTxCommit, error)

	// Gany Chain
	GetChainId() string
	GetGanyTxByUrl(ganyUrlBz []byte) (pb.GanyTx, error)
	QueryBulletinByTimePeriod(typ pb.Bulletin_BulletinType, topicHash [32]byte, startTime, endTime int64,
		excludeSNs map[string]struct{}) ([]*pb.Bulletin, error)
}

var _ GanyApp = &GanyApplication{}

// Bulletin: Type1||TopicHashXX8||Timestamp5||SN8||FromHashXX8 => Topic32||HistoryCount4||IdList||Bulletin
// KeyMap: 220||BlockTime5||TxIndex3[1:3] => [Type1||TopicHashXX8||Timestamp5]...
// SN8: BlockTime5||TxIndex3
// Gany URL: gany://TopicHash4hex.BlockTime5decimal.TxIndex3decimal (hex string)

// GanyTxBz: (TxFieldLengths raw-bytes)16||StochasticPayment||Bulletin||AuthProof||AuthChallenge

type GanyApplication struct {
	db       *badger.DB
	chainId  string
	tmClient *tmhttp.HTTP

	// logger
	logger tmlog.Logger

	// temp block data
	currentBatch          *badger.Txn
	currentHeight         int64
	currentTxIndex        int64
	currentBlockTimestamp int64 // second
}

func NewGanyApplication(db *badger.DB, tmPort string, logger tmlog.Logger) *GanyApplication {
	tmClient, err := tmhttp.New(fmt.Sprintf("http://127.0.0.1:%v", tmPort))
	if err != nil {
		panic(err)
	}

	return &GanyApplication{
		db:       db,
		tmClient: tmClient,
		logger:   logger,
	}
}

// -----------------------------Tendermint Client---------------------------------

func (app *GanyApplication) BroadcastTx(txBz []byte) (*tmcoretypes.ResultBroadcastTxCommit, error) {
	return app.tmClient.BroadcastTxCommit(context.Background(), txBz)
}

// ---------------------------------ABCI------------------------------------------

// Info/Query Connection
func (app *GanyApplication) Info(req abcitypes.RequestInfo) abcitypes.ResponseInfo {
	return abcitypes.ResponseInfo{}
}

func (app *GanyApplication) Query(req abcitypes.RequestQuery) abcitypes.ResponseQuery {
	return abcitypes.ResponseQuery{}
}

// Mempool Connection
func (app *GanyApplication) CheckTx(req abcitypes.RequestCheckTx) abcitypes.ResponseCheckTx {
	_, err := validateGanyTxBz(req.Tx)

	var code uint32
	if err != nil {
		switch err {
		case pb.ErrInvalidTxBytes:
			code = CheckTxCodeErrorInvalidTxBytes
		case pb.ErrInvalidBulletinFields:
			code = CheckTxCodeErrorInvalidBulletin
		case pb.ErrInvalidStochasticPaymentFields:
			code = CheckTxCodeErrorInvalidStochasticPayment
		default:
			code = CheckTxCodeError
		}
		return abcitypes.ResponseCheckTx{Code: code, Log: err.Error()}
	}

	return abcitypes.ResponseCheckTx{Code: CheckTxCodeOK}
}

// Consensus Connection
func (app *GanyApplication) InitChain(req abcitypes.RequestInitChain) abcitypes.ResponseInitChain {
	app.chainId = req.GetChainId()
	return abcitypes.ResponseInitChain{}
}

func (app *GanyApplication) BeginBlock(req abcitypes.RequestBeginBlock) abcitypes.ResponseBeginBlock {
	// 1. get current block info
	// 2. store temp block data
	app.currentHeight = req.Header.GetHeight()
	app.currentBlockTimestamp = req.Header.GetTime().Unix()
	app.currentBatch = app.db.NewTransaction(true)
	app.currentTxIndex = 0
	return abcitypes.ResponseBeginBlock{}
}

func (app *GanyApplication) DeliverTx(req abcitypes.RequestDeliverTx) abcitypes.ResponseDeliverTx {
	_, err := validateGanyTxBz(req.Tx)
	if err != nil {
		var code uint32
		switch err {
		case pb.ErrInvalidTxBytes:
			code = CheckTxCodeErrorInvalidTxBytes
		case pb.ErrInvalidBulletinFields:
			code = CheckTxCodeErrorInvalidBulletin
		case pb.ErrInvalidStochasticPaymentFields:
			code = CheckTxCodeErrorInvalidStochasticPayment
		default:
			code = CheckTxCodeError
		}
		return abcitypes.ResponseDeliverTx{Code: code, Log: err.Error()}
	}

	fmt.Printf("blockTime: %v\n", app.currentBlockTimestamp)
	fmt.Printf("txIndex: %v\n", app.currentTxIndex)

	err = putGanyTx(app.currentBatch, req.Tx, app.currentBlockTimestamp, app.currentTxIndex)
	if err != nil {
		app.logger.Error("put bulletin error", "err", err.Error(), "block height", app.currentHeight, "tx index", app.currentTxIndex)

		var code uint32
		switch err {
		case ErrTimestampTooLong:
			code = DeliverTxCodeErrorTimestampTooLong
		case ErrInvalidOldSN:
			code = DeliverTxCodeErrorInvalidOldSN
		case ErrCantFindOldBulletin:
			code = DeliverTxCodeErrorCannotFindOldBulletin
		case ErrCantOverwriteBulletin:
			code = DeliverTxCodeErrorCannotOverwriteBulletin
		default:
			code = DeliverTxCodeErrorOther
		}
		return abcitypes.ResponseDeliverTx{Code: code, Log: err.Error()}
	}

	app.currentTxIndex++
	return abcitypes.ResponseDeliverTx{Code: CheckTxCodeOK}
}

func (app *GanyApplication) EndBlock(req abcitypes.RequestEndBlock) abcitypes.ResponseEndBlock {
	return abcitypes.ResponseEndBlock{}
}

func (app *GanyApplication) Commit() abcitypes.ResponseCommit {
	app.currentBatch.Commit()
	return abcitypes.ResponseCommit{Data: []byte{}}
}

// State Sync Connection
func (app *GanyApplication) ListSnapshots(abcitypes.RequestListSnapshots) abcitypes.ResponseListSnapshots {
	return abcitypes.ResponseListSnapshots{}
}

func (app *GanyApplication) OfferSnapshot(abcitypes.RequestOfferSnapshot) abcitypes.ResponseOfferSnapshot {
	return abcitypes.ResponseOfferSnapshot{}
}

func (app *GanyApplication) LoadSnapshotChunk(abcitypes.RequestLoadSnapshotChunk) abcitypes.ResponseLoadSnapshotChunk {
	return abcitypes.ResponseLoadSnapshotChunk{}
}

func (app *GanyApplication) ApplySnapshotChunk(abcitypes.RequestApplySnapshotChunk) abcitypes.ResponseApplySnapshotChunk {
	return abcitypes.ResponseApplySnapshotChunk{}
}

// ---------------------------------Backend------------------------------------------

func (app *GanyApplication) GetChainId() string {
	return app.chainId
}

func (app *GanyApplication) GetGanyTxByUrl(ganyUrlBz []byte) (pb.GanyTx, error) {
	var tx pb.GanyTx
	var err error

	txErr := app.db.View(func(txn *badger.Txn) error {
		tx, err = getGanyTx(txn, ganyUrlBz)
		if err != nil {
			return err
		}
		return nil
	})

	if txErr != nil {
		return nil, txErr
	}
	return tx, nil
}

func (app *GanyApplication) QueryBulletinByTimePeriod(typ pb.Bulletin_BulletinType, topicHash [32]byte, startTime, endTime int64,
	excludeSNs map[string]struct{}) ([]*pb.Bulletin, error) {

	var results []*pb.Bulletin
	var err error

	txErr := app.db.View(func(txn *badger.Txn) error {
		results, _, err = queryBulletins(txn, byte(typ), topicHash, startTime, endTime, excludeSNs)
		if err != nil {
			return err
		}
		return nil
	})

	if txErr != nil {
		return nil, txErr
	}

	return results, nil
}

// ---------------------------------Data------------------------------------------

func validateGanyTxBz(ganyTx pb.GanyTx) (bool, error) {
	return ganyTx.IsValid()
}

// Given GanyURL(TopicHash4||BlockTime5||TxIndex3), return the bulletin
func getGanyTx(txn *badger.Txn, ganyUrlBz []byte) (ganyTx pb.GanyTx, err error) {
	// lookup mainKeyHead range
	key := append([]byte{MainKeyHeadByte}, ganyUrlBz[4:11]...)
	item, err := txn.Get(key)
	if err == badger.ErrKeyNotFound {
		return nil, ErrKeyNotFound
	} else if err != nil {
		return nil, err
	}

	// lookup mainKeyHead
	txIndex := int(ganyUrlBz[11])
	mainKey := make([]byte, MainKeyLen)
	err = item.Value(func(value []byte) error {
		mainKeyHead := lookupMainKeyFromRange(value, txIndex)
		if len(mainKeyHead) == 0 {
			return ErrMainKeyHeadNotFound
		}
		copy(mainKey[:MainKeyHeadLen], mainKeyHead)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// lookup bulletin
	copy(mainKey[MainKeyHeadLen:], ganyUrlBz[4:])
	iter := txn.NewIterator(badger.DefaultIteratorOptions)
	defer iter.Close()

	prefix := mainKey[:MainKeyHeadLen+8]
	for iter.Seek(prefix); iter.ValidForPrefix(prefix); iter.Next() {
		it := iter.Item()
		err = it.Value(func(value []byte) error {
			count := int(binary.BigEndian.Uint32(value[TopicHashEnd:HistoryCountEnd]))
			txStart := HistoryCountEnd + count*BulletinIdLen
			ganyTx = append(ganyTx, value[txStart:]...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}

	return ganyTx, err
}

func putGanyTx(txn *badger.Txn, ganyTx pb.GanyTx, blockTimestamp, txIndex int64) (err error) {
	bulletin, err := ganyTx.GetBulletin()
	if err != nil {
		return err
	}

	if len(bulletin.OldSn) == 0 {
		err = createGanyTx(txn, ganyTx, blockTimestamp, txIndex) // create new bulletin
	} else {
		err = overwriteBulletin(txn, ganyTx) // update or delete existing bulletin
	}
	return
}

func createGanyTx(txn *badger.Txn, ganyTx pb.GanyTx, blockTimestamp, txIndex int64) error {
	bulletin, err := ganyTx.GetBulletin()
	if err != nil {
		return err
	}

	if len(bulletin.ContentList) == 0 { //do nothing for empty bulletin
		return nil
	}
	var sn [8]byte
	var timeBuf [8]byte
	var indexBuf [8]byte
	binary.BigEndian.PutUint64(timeBuf[:], uint64(blockTimestamp))
	binary.BigEndian.PutUint64(indexBuf[:], uint64(txIndex))
	copy(sn[:5], timeBuf[3:])
	copy(sn[5:], indexBuf[5:])

	fmt.Printf("sn: %v\n", sn)

	// if it's not censor type, check CreateTime
	if bulletin.GetType() < pb.Bulletin_CENSOR && bulletin.GetTimestamp() < blockTimestamp-MaxTimeDiff {
		return ErrTimestampTooLong
	}

	// record bulletin
	var bKey [MainKeyLen]byte
	copy(bKey[:], getMainKeyHead(bulletin))
	copy(bKey[MainKeyHeadLen:], sn[:])
	copy(bKey[MainKeyHeadLen+8:], sum64(bulletin.GetFrom()[:]))

	bValue := make([]byte, 0, TopicHashLen+HisCountLen+BulletinIdLen+ganyTx.Len())
	topicHash := bulletin.GetTopicHash()
	bValue = append(bValue, topicHash[:]...)
	bValue = append(bValue, 0, 0, 0, 1) // count=1
	id, err := ganyTx.GetBulletinID()
	if err != nil {
		return err
	}

	bValue = append(bValue, id[:]...)
	bValue = append(bValue, ganyTx...)
	err = txn.SetEntry(badger.NewEntry(bKey[:], bValue).WithTTL(toValidTTL(bulletin.GetDuration())))
	if err != nil {
		return err
	}

	// record main key map
	key := append(append([]byte{MainKeyHeadByte}, timeBuf[3:]...), indexBuf[5:]...)
	err = txn.SetEntry(badger.NewEntry(key, bKey[:MainKeyHeadLen]).
		WithTTL(toValidTTL(bulletin.GetDuration())))
	if err != nil {
		return err
	}

	// record main key range
	rangeKey := append(append([]byte{MainKeyHeadByte}, timeBuf[3:]...), indexBuf[5:7]...)
	mainKeyRangeItem, err := txn.Get(rangeKey)
	if err != nil && err != badger.ErrKeyNotFound {
		return err
	}

	// main key map is empty, set it directly
	if err == badger.ErrKeyNotFound {
		return txn.SetEntry(badger.NewEntry(rangeKey, bKey[:MainKeyHeadLen]).
			WithTTL(toValidTTL(bulletin.GetDuration())))
	}

	// if main key map is not empty, append the new key into the map
	var oldMainKeyRange []byte
	oldExpireAt := mainKeyRangeItem.ExpiresAt()
	err = mainKeyRangeItem.Value(func(value []byte) error {
		copy(oldMainKeyRange, value)
		return nil
	})

	var newMainKeyRangeBuffer bytes.Buffer
	newMainKeyRangeBuffer.Write(oldMainKeyRange)
	newMainKeyRangeBuffer.Write(bKey[:MainKeyHeadLen])
	newMainKeyRange := newMainKeyRangeBuffer.Bytes()

	newExpireAt := uint64(time.Now().Add(toValidTTL(bulletin.GetDuration())).Unix())
	if newExpireAt > oldExpireAt {
		return txn.SetEntry(badger.NewEntry(rangeKey, newMainKeyRange).
			WithTTL(toValidTTL(bulletin.GetDuration())))
	}
	return txn.SetEntry(badger.NewEntry(rangeKey, newMainKeyRange).
		WithTTL(time.Duration(int64(oldExpireAt) - time.Now().Unix())))
}

func getOldVersionOfGanyTx(txn *badger.Txn, newTx pb.GanyTx) (idHis, mainKey []byte,
	oldTx pb.GanyTx, err error) {

	newBulletin, err := newTx.GetBulletin()
	if err != nil {
		return
	}

	mainKey = make([]byte, MainKeyLen)
	copy(mainKey[:], getMainKeyHead(newBulletin))
	copy(mainKey[MainKeyHeadLen:], newBulletin.GetOldSn())
	copy(mainKey[MainKeyHeadLen+8:], sum64(newBulletin.GetFrom()[:]))
	item, err := txn.Get(mainKey[:])
	if err != nil {
		return
	}

	id, err := newTx.GetBulletinID()
	if err != nil {
		return
	}

	err = item.Value(func(value []byte) error {
		ok, txStart := historyContainsId(value, id)
		if !ok {
			return ErrInvalidOldSN
		}
		idHis = value[HistoryCountEnd:txStart]
		oldTx = append(oldTx, value[txStart:]...)
		return err
	})
	return
}

func overwriteBulletin(txn *badger.Txn, newTx pb.GanyTx) error {
	idHis, key, oldTx, err := getOldVersionOfGanyTx(txn, newTx)
	if err != nil {
		return err
	}
	if oldTx == nil {
		return ErrCantFindOldBulletin
	}

	oldBulletin, err := oldTx.GetBulletin()
	if err != nil {
		return err
	}

	newBulletin, err := newTx.GetBulletin()
	if err != nil {
		return err
	}
	if !oldBulletin.CanBeOverwrittenBy(newBulletin) {
		return ErrCantOverwriteBulletin
	}

	// update
	if len(newBulletin.GetContentList()) != 0 {

		value := make([]byte, 32+4, 32+4+len(idHis)+BulletinIdLen+newTx.Len())
		topicHash := newBulletin.GetTopicHash()
		copy(value[:32], topicHash[:])
		binary.BigEndian.PutUint32(value[32:36], uint32(len(idHis)/BulletinIdLen+1))
		value = append(value, idHis...)
		newId, err := newTx.GetBulletinID()
		if err != nil {
			return err
		}

		value = append(value, newId[:]...)
		value = append(value, newTx...)
		return txn.SetEntry(badger.NewEntry(key, value).
			WithTTL(toValidTTL(newBulletin.GetDuration())))
	}

	// delete
	return txn.Delete(key)
}

// Get the bulletins with type and topicHash, between [startTime, endTime],
// excluding some given SNs (which are deleted by censors, or have been got before)
func queryBulletins(txn *badger.Txn, typ byte, topicHash [32]byte, startTime, endTime int64,
	excludeSNs map[string]struct{}) ([]*pb.Bulletin, int, error) {

	result := make([]*pb.Bulletin, 0, DefaultValueLength)
	keyStart := make([]byte, MainKeyLen)
	keyStart[0] = typ
	copy(keyStart[1:], sum64(topicHash[:]))
	keyEnd := make([]byte, MainKeyLen)
	copy(keyEnd[:MainKeyHeadLen], keyStart[:MainKeyHeadLen]) // same prefix

	var stBuf [8]byte
	var etBuf [8]byte
	binary.BigEndian.PutUint64(stBuf[:], uint64(startTime))
	binary.BigEndian.PutUint64(etBuf[:], uint64(endTime))
	copy(keyStart[9:14], stBuf[3:])
	copy(keyEnd[9:14], etBuf[3:])
	copy(keyStart[14:], []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	copy(keyEnd[14:], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = false
	opts.Reverse = true
	iter := txn.NewIterator(opts)
	defer iter.Close()

	num := 0
	tmpTxPool := &sync.Pool{New: func() interface{} {
		return pb.GanyTx{}
	}}

	for iter.Seek(keyEnd); iter.Valid(); iter.Next() {
		item := iter.Item()
		if bytes.Compare(keyStart, item.Key()) > 0 {
			break
		}
		keyHex := hexutil.Encode(item.Key()[MainKeyHeadLen : MainKeyHeadLen+8])
		if _, ok := excludeSNs[keyHex]; ok {
			continue // don't return the excluded ones
		}
		err := item.Value(func(value []byte) error {
			if !bytes.Equal(value[:32], topicHash[:]) {
				return nil //incorrect topHash is possible because of hash-conflicting
			}
			count := int(binary.BigEndian.Uint32(value[TopicHashEnd:HistoryCountEnd]))
			txStart := HistoryCountEnd + count*BulletinIdLen
			tmpTx := tmpTxPool.Get().(pb.GanyTx)
			tmpTx = append(tmpTx, value[txStart:]...)
			b, err := tmpTx.GetBulletin()
			if err != nil {
				return err
			}
			result = append(result, b)
			num++

			// clean tx pool
			tmpTxPool.Put(pb.GanyTx{})
			return nil
		})

		if err != nil {
			return nil, 0, err
		}
		if len(result) > DefaultValueLength {
			break
		}
		if num > MaxQueryResultCount {
			break
		}
	}
	return result, num, nil
}

// ----------------------------------------------------------------

func sum64(bz []byte) []byte {
	h := xxhash.New()
	return h.Sum(bz)
}

func toValidTTL(expire int64) (ttl time.Duration) {
	now := time.Now().Unix()
	if expire > now {
		ttl = time.Duration(expire-now) * time.Second
		if ttl > MaxTTL {
			ttl = MaxTTL
		}
	} else {
		ttl = MinTTL
	}
	return
}

// Given a bulletin's id, check whether it was once stored in an item's value.
func historyContainsId(value []byte, id [64]byte) (bool, int) {
	count := int(binary.BigEndian.Uint32(value[TopicHashEnd:HistoryCountEnd]))
	end := HistoryCountEnd + count*BulletinIdLen
	for start := HistoryCountEnd; start < end; start += BulletinIdLen {
		if bytes.Equal(value[start:start+BulletinIdLen], id[:]) {
			return true, end
		}
	}
	return false, end
}

func getMainKeyHead(bulletin *pb.Bulletin) []byte {
	mainKeyHead := make([]byte, MainKeyHeadLen)
	mainKeyHead[0] = byte(bulletin.Type) // Type1
	topicHash := bulletin.GetTopicHash()
	copy(mainKeyHead[1:], sum64(topicHash[:])) // TopicHashXX8
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(bulletin.Timestamp))
	copy(mainKeyHead[9:], buf[3:]) //Timestamp5
	return mainKeyHead
}

func lookupMainKeyFromRange(mainKeyRange []byte, index int) []byte {
	// ensure the index is in range of mainKeyRange
	if len(mainKeyRange) < (index+1)*MainKeyHeadLen {
		return nil
	}
	return mainKeyRange[index*MainKeyHeadLen : (index+1)*MainKeyHeadLen]
}
