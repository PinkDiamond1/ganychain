package app

import (
	"encoding/binary"
	"os"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"

	pb "github.com/smartbch/ganychain/proto"
)

const (
	TestDataDir = "../testdata/app"
)

var (
	TestAddress        = common.HexToAddress("0x06C14ED469FB93545cbF071b593D8f90194Ede62")
	WrongAddress       = common.HexToAddress("0x6f36Cf5520b10F77a92a72276983d9f0E6327E31")
	TimeNow            = time.Now().Truncate(24 * time.Hour)
	TimestampYesterday = uint64(TimeNow.Add(-time.Hour * 24).Unix())
	TimestampNow       = TimeNow.Unix()
	TimestampDuration  = TimeNow.Add(time.Hour).Unix()
	TimestampTomorrow  = uint64(TimeNow.Add(time.Hour * 24).Unix())

	TimestampBlockOne = TimeNow.Add(time.Second * 10).Unix()
	TimestampBlockTwo = TimeNow.Add(time.Second * 20).Unix()
)

func genSerialBytes(blockTimestamp, txIndex int64) [8]byte {
	var sn [8]byte
	var timeBuf [8]byte
	var indexBuf [8]byte
	binary.BigEndian.PutUint64(timeBuf[:], uint64(blockTimestamp))
	binary.BigEndian.PutUint64(indexBuf[:], uint64(txIndex))
	copy(sn[:5], timeBuf[3:])
	copy(sn[5:], indexBuf[5:])
	return sn
}

// -----------------------------Normal Cases--------------------------------

func TestCreateBulletin(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	b := &pb.Bulletin{
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

	ganyTx := pb.CreateGanyTx(nil, b, nil, nil)

	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, ganyTx, TimestampBlockOne, 0)
	})
	require.NoError(t, txErr)

	var txFromDB pb.GanyTx
	txErr = db.View(func(txn *badger.Txn) error {
		var bKey [MainKeyLen]byte
		copy(bKey[:], getMainKeyHead(b))
		sn := genSerialBytes(TimestampBlockOne, 0)
		copy(bKey[MainKeyHeadLen:], sn[:])
		copy(bKey[MainKeyHeadLen+8:], sum64(TestAddress.Bytes()))
		item, err := txn.Get(bKey[:])
		require.NoError(t, err)

		id, err := ganyTx.GetBulletinID()
		require.NoError(t, err)

		err = item.Value(func(value []byte) error {
			ok, txStart := historyContainsId(value, id)
			require.True(t, ok)
			txFromDB = append(txFromDB, value[txStart:]...)
			require.EqualValues(t, ganyTx, txFromDB)
			return err
		})
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, txErr)

	bBz, _ := proto.Marshal(b)
	bFromDBBz, err := txFromDB.GetBulletinBytes()
	require.NoError(t, err)

	require.EqualValues(t, bBz, bFromDBBz)
}

func TestOverwriteBulletin(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

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

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)

	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx1, TimestampBlockOne, 0)
	})
	require.NoError(t, txErr)

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
			{3, 4},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx2 := pb.CreateGanyTx(nil, b2, nil, nil)

	txErr = db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx2, TimestampBlockOne, 1)
	})
	require.NoError(t, txErr)

	var txFromDB pb.GanyTx
	txErr = db.View(func(txn *badger.Txn) error {
		var bKey [MainKeyLen]byte
		copy(bKey[:], getMainKeyHead(b2))
		copy(bKey[MainKeyHeadLen:], sn[:])
		copy(bKey[MainKeyHeadLen+8:], sum64(TestAddress[:]))
		item, err := txn.Get(bKey[:])
		require.NoError(t, err)

		id, err := tx2.GetBulletinID()
		require.NoError(t, err)

		err = item.Value(func(value []byte) error {
			ok, txStart := historyContainsId(value, id)
			require.True(t, ok)
			txFromDB = append(txFromDB, value[txStart:]...)
			require.EqualValues(t, tx2, txFromDB)
			return err
		})
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, txErr)

	b2Bz, _ := proto.Marshal(b2)
	bFromDBBz, err := txFromDB.GetBulletinBytes()
	require.NoError(t, err)

	require.EqualValues(t, b2Bz, bFromDBBz)

}

func TestDeleteBulletin(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

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

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx1, TimestampBlockOne, 0)
	})
	require.NoError(t, txErr)

	sn := genSerialBytes(TimestampBlockOne, 0)
	b2 := &pb.Bulletin{
		Type:          pb.Bulletin_BLOG,
		Topic:         []byte{0x12},
		Timestamp:     TimestampNow,
		Duration:      TimestampDuration,
		OldSn:         sn[:],
		From:          TestAddress.Bytes(),
		ContentType:   "My Blog",
		ContentList:   [][]byte{},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx2 := pb.CreateGanyTx(nil, b2, nil, nil)
	txErr = db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx2, TimestampBlockOne, 1)
	})
	require.NoError(t, txErr)

	txErr = db.View(func(txn *badger.Txn) error {
		var bKey [MainKeyLen]byte
		copy(bKey[:], getMainKeyHead(b2))
		copy(bKey[MainKeyHeadLen:], sn[:])
		copy(bKey[MainKeyHeadLen+8:], sum64(TestAddress[:]))
		_, err := txn.Get(bKey[:])
		require.EqualError(t, err, "Key not found")
		return nil
	})
	require.NoError(t, txErr)
}

func TestGetBulletinByGanyUrl(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

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

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx1, TimestampBlockOne, 0)
	})
	require.NoError(t, txErr)

	sn := genSerialBytes(TimestampBlockOne, 0)
	ganyUrlBz := make([]byte, 4+8)
	topicHash := b1.GetTopicHash()
	copy(ganyUrlBz[:4], topicHash[:4])
	copy(ganyUrlBz[4:], sn[:])

	txErr = db.View(func(txn *badger.Txn) error {
		txFromDB, err := getGanyTx(txn, ganyUrlBz)
		require.NoError(t, err)
		b1Bz, _ := proto.Marshal(b1)
		bFromDBBz, _ := txFromDB.GetBulletinBytes()
		require.EqualValues(t, b1Bz, bFromDBBz)
		return nil
	})
	require.NoError(t, txErr)

	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       sn[:],
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{3, 4},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx2 := pb.CreateGanyTx(nil, b2, nil, nil)
	txErr = db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx2, TimestampBlockTwo, 1)
	})
	require.NoError(t, txErr)

	txErr = db.View(func(txn *badger.Txn) error {
		txFromDB, err := getGanyTx(txn, ganyUrlBz)
		require.NoError(t, err)
		b2Bz, _ := proto.Marshal(b2)
		bFromDBBz, _ := txFromDB.GetBulletinBytes()
		require.EqualValues(t, b2Bz, bFromDBBz)
		return nil
	})
	require.NoError(t, txErr)
}

func TestQueryBulletinsByTimePeriod(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

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

	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
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

	b3 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       nil,
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{5, 6},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	tx2 := pb.CreateGanyTx(nil, b2, nil, nil)
	tx3 := pb.CreateGanyTx(nil, b3, nil, nil)

	txErr := db.Update(func(txn *badger.Txn) error {
		err = putGanyTx(txn, tx1, TimestampBlockOne, 0)
		require.NoError(t, err)
		err = putGanyTx(txn, tx2, TimestampBlockOne, 1)
		require.NoError(t, err)
		err = putGanyTx(txn, tx3, TimestampBlockOne, 2)
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, txErr)

	topicHash := b1.GetTopicHash()
	startTime := TimestampYesterday
	endTime := TimestampTomorrow

	txErr = db.View(func(txn *badger.Txn) error {
		results, num, err := queryBulletins(txn, byte(pb.Bulletin_BLOG), topicHash, int64(startTime), int64(endTime), map[string]struct{}{})
		require.NoError(t, err)
		require.EqualValues(t, 3, num)

		b3Bz, _ := proto.Marshal(b3)
		b3FromDBBz, _ := proto.Marshal(results[0])
		require.EqualValues(t, b3Bz, b3FromDBBz)

		b2Bz, _ := proto.Marshal(b2)
		b2FromDBBz, _ := proto.Marshal(results[1])
		require.EqualValues(t, b2Bz, b2FromDBBz)

		b1Bz, _ := proto.Marshal(b1)
		b1FromDBBz, _ := proto.Marshal(results[2])
		require.EqualValues(t, b1Bz, b1FromDBBz)
		return nil
	})
	require.NoError(t, txErr)
}

func TestQueryBulletinsWithExcludeSNs(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

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

	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
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

	b3 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow,
		Duration:    TimestampDuration,
		OldSn:       nil,
		From:        TestAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{5, 6},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	tx2 := pb.CreateGanyTx(nil, b2, nil, nil)
	tx3 := pb.CreateGanyTx(nil, b3, nil, nil)
	txErr := db.Update(func(txn *badger.Txn) error {
		err = putGanyTx(txn, tx1, TimestampBlockOne, 0)
		require.NoError(t, err)
		err = putGanyTx(txn, tx2, TimestampBlockOne, 1)
		require.NoError(t, err)
		err = putGanyTx(txn, tx3, TimestampBlockOne, 2)
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, txErr)

	topicHash := b1.GetTopicHash()
	startTime := TimestampYesterday
	endTime := TimestampTomorrow

	// exclude SN01
	sn := genSerialBytes(TimestampBlockOne, 0)
	txErr = db.View(func(txn *badger.Txn) error {
		results, num, err := queryBulletins(txn, byte(pb.Bulletin_BLOG), topicHash, int64(startTime), int64(endTime), map[string]struct{}{
			hexutil.Encode(sn[:]): {},
		})
		require.NoError(t, err)
		require.EqualValues(t, 2, num)

		b3Bz, _ := proto.Marshal(b3)
		b3FromDBBz, _ := proto.Marshal(results[0])
		require.EqualValues(t, b3Bz, b3FromDBBz)

		b2Bz, _ := proto.Marshal(b2)
		b2FromDBBz, _ := proto.Marshal(results[1])
		require.EqualValues(t, b2Bz, b2FromDBBz)
		return nil
	})
	require.NoError(t, txErr)
}

// -----------------------------Bad Cases--------------------------------

func TestGetBulletinWithWrongGanyUrl(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

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

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx1, TimestampBlockOne, 0)
	})
	require.NoError(t, txErr)

	sn2 := genSerialBytes(TimestampBlockTwo, 0)
	wrongGanyUrlBz := make([]byte, 4+8)
	topicHash := b1.GetTopicHash()
	copy(wrongGanyUrlBz[:4], topicHash[:4])
	copy(wrongGanyUrlBz[4:], sn2[:])

	txErr = db.View(func(txn *badger.Txn) error {
		_, err := getGanyTx(txn, wrongGanyUrlBz)
		require.EqualError(t, err, ErrKeyNotFound.Error())
		return nil
	})
	require.NoError(t, txErr)
}

func TestCreateBulletinWithEmptyInfoList(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	b1 := &pb.Bulletin{
		Type:          pb.Bulletin_BLOG,
		Topic:         []byte{0x12},
		Timestamp:     TimestampNow,
		Duration:      TimestampDuration,
		OldSn:         nil,
		From:          TestAddress.Bytes(),
		ContentType:   "My Blog",
		ContentList:   [][]byte{},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx1, TimestampBlockOne, 0)
	})
	require.NoError(t, txErr)

	sn := genSerialBytes(TimestampBlockOne, 0)
	txErr = db.View(func(txn *badger.Txn) error {
		var bKey [MainKeyLen]byte
		copy(bKey[:], getMainKeyHead(b1))
		copy(bKey[MainKeyHeadLen:], sn[:])
		_, err := txn.Get(bKey[:])
		require.EqualError(t, err, badger.ErrKeyNotFound.Error())
		return nil
	})
	require.NoError(t, txErr)
}

func TestCreateBulletinWithTooLongTimestamp(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	b1 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow - (MaxTimeDiff + 1),
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

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx1, TimestampBlockOne, 0)
	})
	require.EqualError(t, txErr, ErrTimestampTooLong.Error())

	sn := genSerialBytes(TimestampBlockOne, 0)
	txErr = db.View(func(txn *badger.Txn) error {
		var bKey [MainKeyLen]byte
		copy(bKey[:], getMainKeyHead(b1))
		copy(bKey[MainKeyHeadLen:], sn[:])
		_, err := txn.Get(bKey[:])
		require.EqualError(t, err, badger.ErrKeyNotFound.Error())
		return nil
	})
	require.NoError(t, txErr)
}

func TestOverwriteBulletinFailed(t *testing.T) {
	db, err := badger.Open(badger.DefaultOptions(TestDataDir))
	require.NoError(t, err)
	defer cleanData(db)

	b1 := &pb.Bulletin{
		Type:          pb.Bulletin_BLOG,
		Topic:         []byte{0x12},
		Timestamp:     TimestampNow - (MaxTimeDiff + 1),
		Duration:      TimestampDuration,
		OldSn:         nil,
		From:          TestAddress.Bytes(),
		ContentType:   "My Blog",
		ContentList:   [][]byte{},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx1 := pb.CreateGanyTx(nil, b1, nil, nil)
	txErr := db.Update(func(txn *badger.Txn) error {
		return putGanyTx(txn, tx1, TimestampBlockOne, 0)
	})
	require.NoError(t, txErr)

	sn := genSerialBytes(TimestampBlockOne, 0)
	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   TimestampNow - (MaxTimeDiff + 1),
		Duration:    TimestampDuration,
		OldSn:       sn[:],
		From:        WrongAddress.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{3, 4},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx2 := pb.CreateGanyTx(nil, b2, nil, nil)
	txErr = db.Update(func(txn *badger.Txn) error {
		err = putGanyTx(txn, tx2, TimestampBlockOne, 1)
		require.EqualError(t, err, badger.ErrKeyNotFound.Error())
		return nil
	})
	require.NoError(t, txErr)
}

func cleanData(db *badger.DB) {
	db.DropAll()
	db.Close()
	os.RemoveAll("../testdata")
}
