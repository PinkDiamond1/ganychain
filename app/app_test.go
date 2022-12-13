package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tpbtypes "github.com/tendermint/tendermint/proto/tendermint/types"

	pb "github.com/smartbch/ganychain/proto"
)

func makeFakeEmptyBytes(length int) []byte {
	return bytes.Repeat([]byte{0x00}, length)
}

func CreateTestApp(db *badger.DB) *GanyApplication {
	return NewGanyApplication(db, "10000", tmlog.MustNewDefaultLogger(tmlog.LogFormatPlain, tmlog.LogLevelInfo, false))
}

// ----------------------------Normal Cases------------------------------------
func TestCheckTx(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	ganyApp := CreateTestApp(db)

	sp1 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: makeFakeEmptyBytes(32),
		DueTime:                 TimestampDuration,
		Probability:             1000,
		Nonces:                  makeFakeEmptyBytes(32),
		Payee:                   TestAddress.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x14"),
		Signature:               makeFakeEmptyBytes(65),
	}

	b1 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       nil,
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}
	tx1 := pb.CreateGanyTx(sp1, b1, nil, nil)
	resp := ganyApp.CheckTx(abcitypes.RequestCheckTx{
		Tx:   tx1,
		Type: abcitypes.CheckTxType_New,
	})
	require.EqualValues(t, CheckTxCodeOK, resp.Code)

	sp2 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: makeFakeEmptyBytes(32),
		DueTime:                 TimestampDuration,
		Probability:             1000,
		Nonces:                  makeFakeEmptyBytes(32),
		Payee:                   TestAddress.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x14"),
		Signature:               makeFakeEmptyBytes(65),
	}

	sn := genSerialBytes(TimestampBlockOne, 0)
	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       sn[:],
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}
	tx2 := pb.CreateGanyTx(sp2, b2, nil, nil)
	resp = ganyApp.CheckTx(abcitypes.RequestCheckTx{
		Tx:   tx2,
		Type: abcitypes.CheckTxType_New,
	})
	require.EqualValues(t, CheckTxCodeOK, resp.Code)
}

func TestGanyAppBasic(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	ganyApp := CreateTestApp(db)

	// ---------------------Height 1-----------------
	// 1. BeginBlock
	ganyApp.BeginBlock(abcitypes.RequestBeginBlock{
		Header: tpbtypes.Header{
			Height: 1,
			Time:   time.Unix(TimestampBlockOne, 0).UTC(),
		},
	})

	// 2. DeliverTx
	sp1 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: makeFakeEmptyBytes(32),
		DueTime:                 TimestampDuration,
		Probability:             1000,
		Nonces:                  makeFakeEmptyBytes(32),
		Payee:                   TestAddress.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x14"),
		Signature:               makeFakeEmptyBytes(65),
	}

	b1 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       nil,
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}
	tx1 := pb.CreateGanyTx(sp1, b1, nil, nil)
	deliverTxResp := ganyApp.DeliverTx(abcitypes.RequestDeliverTx{
		Tx: tx1,
	})
	require.EqualValues(t, CheckTxCodeOK, deliverTxResp.Code)

	sp2 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: makeFakeEmptyBytes(32),
		DueTime:                 TimestampDuration,
		Probability:             1000,
		Nonces:                  makeFakeEmptyBytes(32),
		Payee:                   TestAddress.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x14"),
		Signature:               makeFakeEmptyBytes(65),
	}

	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x34},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       nil,
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{3, 4},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}
	tx2 := pb.CreateGanyTx(sp2, b2, nil, nil)
	deliverTxResp = ganyApp.DeliverTx(abcitypes.RequestDeliverTx{
		Tx: tx2,
	})
	require.EqualValues(t, CheckTxCodeOK, deliverTxResp.Code)

	// 3. EndBlock
	ganyApp.EndBlock(abcitypes.RequestEndBlock{
		Height: 1,
	})

	// 4. Commit
	ganyApp.Commit()

	var bKey1, bKey2 [MainKeyLen]byte
	copy(bKey1[:], getMainKeyHead(b1))
	copy(bKey2[:], getMainKeyHead(b2))
	sn1 := genSerialBytes(TimestampBlockOne, 0)
	sn2 := genSerialBytes(TimestampBlockOne, 1)
	copy(bKey1[MainKeyHeadLen:], sn1[:])
	copy(bKey1[MainKeyHeadLen+8:], sum64(TestAddress.Bytes()))
	copy(bKey2[MainKeyHeadLen:], sn2[:])
	copy(bKey2[MainKeyHeadLen+8:], sum64(TestAddress.Bytes()))

	txErr1 := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(bKey1[:])
		if err != nil {
			return err
		} else {
			return item.Value(func(val []byte) error {
				require.EqualValues(t, tx1, val[HistoryCountEnd+BulletinIdLen:])
				return nil
			})
		}
	})
	require.NoError(t, txErr1)

	txErr2 := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(bKey2[:])
		if err != nil {
			return err
		} else {
			return item.Value(func(val []byte) error {
				require.EqualValues(t, tx2, val[HistoryCountEnd+BulletinIdLen:])
				return nil
			})
		}
	})
	require.NoError(t, txErr2)

	// ---------------------Height 2-----------------
	// 1. BeginBlock
	ganyApp.BeginBlock(abcitypes.RequestBeginBlock{
		Header: tpbtypes.Header{
			Height: 2,
			Time:   time.Unix(TimestampBlockTwo, 0).UTC(),
		},
	})

	// 2. DeliverTx
	sp3 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: makeFakeEmptyBytes(32),
		DueTime:                 TimestampDuration,
		Probability:             1000,
		Nonces:                  makeFakeEmptyBytes(32),
		Payee:                   TestAddress.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x14"),
		Signature:               makeFakeEmptyBytes(65),
	}

	b3 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       sn1[:],
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{5, 6},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}
	tx3 := pb.CreateGanyTx(sp3, b3, nil, nil)
	deliverTxResp = ganyApp.DeliverTx(abcitypes.RequestDeliverTx{
		Tx: tx3,
	})
	require.EqualValues(t, CheckTxCodeOK, deliverTxResp.Code)

	// 3. EndBlock
	ganyApp.EndBlock(abcitypes.RequestEndBlock{
		Height: 2,
	})

	// 4. Commit
	ganyApp.Commit()

	// 5. Query
	var bKey3 [MainKeyLen]byte
	copy(bKey3[:], getMainKeyHead(b3))
	copy(bKey3[MainKeyHeadLen:], sn1[:])
	copy(bKey3[MainKeyHeadLen+8:], sum64(TestAddress[:]))

	txErr3 := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(bKey3[:])
		if err != nil {
			return err
		} else {
			return item.Value(func(val []byte) error {
				require.EqualValues(t, tx3, val[HistoryCountEnd+BulletinIdLen*2:])
				return nil
			})
		}
	})
	require.NoError(t, txErr3)
}

// ----------------------------Bad Cases------------------------------------
func TestCheckTxWithInvalidBytes(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	ganyApp := CreateTestApp(db)

	sp1 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: makeFakeEmptyBytes(32),
		DueTime:                 TimestampDuration,
		Probability:             1000,
		Nonces:                  makeFakeEmptyBytes(32),
		Payee:                   TestAddress.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x14"),
		Signature:               makeFakeEmptyBytes(65),
	}

	b1 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       nil,
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}
	tx1 := pb.CreateGanyTx(sp1, b1, nil, nil)
	resp := ganyApp.CheckTx(abcitypes.RequestCheckTx{
		Tx:   tx1[1:],
		Type: abcitypes.CheckTxType_New,
	})
	require.EqualValues(t, CheckTxCodeErrorInvalidTxBytes, resp.Code)
}

func TestCheckTxWithInvalidBulletin(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	ganyApp := CreateTestApp(db)

	sp1 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: makeFakeEmptyBytes(32),
		DueTime:                 TimestampDuration,
		Probability:             1000,
		Nonces:                  makeFakeEmptyBytes(32),
		Payee:                   TestAddress.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x14"),
		Signature:               makeFakeEmptyBytes(65),
	}

	b1 := &pb.Bulletin{
		Type:        -1,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       nil,
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}
	tx1 := pb.CreateGanyTx(sp1, b1, nil, nil)
	resp := ganyApp.CheckTx(abcitypes.RequestCheckTx{
		Tx:   tx1,
		Type: abcitypes.CheckTxType_New,
	})
	require.EqualValues(t, CheckTxCodeErrorInvalidBulletin, resp.Code)
}
