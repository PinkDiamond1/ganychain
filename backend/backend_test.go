package backend_test

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v3"
	gethcmn "github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/protobuf/proto"
	"github.com/holiman/uint256"
	"github.com/smartbch/merkletree"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/sha3"

	"github.com/smartbch/ganychain/app"
	pb "github.com/smartbch/ganychain/proto"
	"github.com/smartbch/ganychain/utils/cryptoutils"
	"github.com/smartbch/ganychain/utils/ethutils"
	"github.com/smartbch/ganychain/utils/testutils"
)

const (
	NumOfShards         = 2
	TestDataDirTemplate = "../testdata/backend/shard%v"

	Prob32 = uint32(0xFFFFFFFF) // 100%
)

var (
	validator    = gethcmn.HexToAddress("0x423403784Ca5bD868731d604Ad097f126B36CAe2")
	main         = gethcmn.HexToAddress("0x06C14ED469FB93545cbF071b593D8f90194Ede62")
	payer        = gethcmn.HexToAddress("0x6f36Cf5520b10F77a92a72276983d9f0E6327E31")
	payee        = gethcmn.HexToAddress("0xE0F007dab8543052dfc4C23Cf8a3aDb848A875f9")
	contractAddr = gethcmn.HexToAddress("0x4EFc88A2c0F05c2b3fe162a5212774597118f241")
	token        = gethcmn.HexToAddress("0x784D2eBe7d7Ec4dD0d63b767AE4CAa9ABeeB00a9")

	validatorPrivateKey, _ = gethcrypto.HexToECDSA("5f41e5ff714e6e9df08fefd36931b908c04b4fd6d90d70223abd85128f56afb9") // local bch test key
	mainPrivateKey, _      = gethcrypto.HexToECDSA("85d8e1398312704c5edff03f626d00556620d041d3f86bab2e943a7fe2b31611") // local bch test key
	payerPrivateKey, _     = gethcrypto.HexToECDSA("0eec7081343dba52a5e117f82f58fe7a2bc1fd156e1e858ff52aea5b121746cd") // local bch test key
	payeePrivateKey, _     = gethcrypto.HexToECDSA("ad3bbcddb9818be5eef2e22c28aaa8c7034cc610a8ad9cff1751201a4e2c25eb") // local bch test key

	amountToValidator = gethcmn.FromHex("0x14") // 20
)

func getCompressedValidatorPubKeys() [][]byte {
	compressedValidatorPubKey := gethcrypto.CompressPubkey(&validatorPrivateKey.PublicKey)
	compressedMainPubKey := gethcrypto.CompressPubkey(&mainPrivateKey.PublicKey)
	compressedPayerPubKey := gethcrypto.CompressPubkey(&payerPrivateKey.PublicKey)
	compressedPayeePubKey := gethcrypto.CompressPubkey(&payeePrivateKey.PublicKey)
	return [][]byte{compressedValidatorPubKey, compressedMainPubKey, compressedPayerPubKey, compressedPayeePubKey}
}

func genMerkleTree(compressedPubKeys [][]byte) (*merkletree.MerkleTree, error) {
	leaves := make([]merkletree.Content, 0, len(compressedPubKeys))
	for _, cpk := range compressedPubKeys {
		xy, err := cryptoutils.CompressedPubKeyToXY(cpk)
		if err != nil {
			return nil, err
		}

		leaves = append(leaves, cryptoutils.MerkleLeaf{Bz: xy})
	}
	tree, _ := merkletree.NewTreeWithHashStrategy(leaves, sha3.NewLegacyKeccak256)
	return tree, nil
}

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

// ----------------------------------------------------------------

// Payer posts a blog, and he/she needs to pay some amount to validator.
func TestPutBulletin_Blog(t *testing.T) {
	apps := make([]app.GanyApp, NumOfShards)
	dbs := make([]*badger.DB, NumOfShards)

	for i := 0; i < NumOfShards; i++ {
		dbPath := fmt.Sprintf(TestDataDirTemplate, i)
		db, err := badger.Open(badger.DefaultOptions(dbPath))
		require.NoError(t, err)

		dbs[i] = db
		apps[i] = testutils.CreateMockGanyApp(db)
	}

	defer cleanData(dbs)

	validatorCompressedPubKeys := getCompressedValidatorPubKeys()
	mockClient := testutils.NewMockClient()
	mockFollower := testutils.NewMockFollower(map[gethcmn.Address]gethcmn.Address{main: payer}, validatorCompressedPubKeys)
	mockBackend := testutils.NewMockBackend(apps, mockFollower, mockClient, token, contractAddr, validator, validatorPrivateKey)

	delegatedAddr, err := mockBackend.GetDelegatedAddr(main)
	require.NoError(t, err)
	require.Equal(t, payer.Hex(), delegatedAddr.Hex())

	mockFollower.SetWallet(payer, uint256.NewInt(0), uint256.NewInt(100))

	payerNonces, payerBalance, err := mockBackend.LoadWalletInStochasticPay(token, payer)
	payerNoncesBz := payerNonces.Bytes32()
	require.NoError(t, err)

	fmt.Printf("payerNonces: %v\n", payerNoncesBz)
	fmt.Printf("payerBalance: %v\n", payerBalance.Uint64())
	require.Equal(t, uint64(100), payerBalance.Uint64())

	tree, _ := genMerkleTree(validatorCompressedPubKeys)
	now := time.Now()
	sp := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             Prob32,
		Nonces:                  payerNoncesBz[:],
		Payee:                   nil,
		AmountToPayee:           gethcmn.FromHex("0x00"),
		AmountToValidator:       amountToValidator,
		Signature:               make([]byte, 65),
	}

	msg := sp.GenEIP712MsgForAB(token)
	typedData := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg, contractAddr)
	eip712Hash, err := ethutils.GetTypedDataHash(typedData)
	require.NoError(t, err)

	sig, err := ethutils.SignWithEIP712Hash(eip712Hash, payerPrivateKey)
	require.NoError(t, err)
	copy(sp.Signature[:], sig)

	b := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   now.Unix(),
		Duration:    now.Add(time.Hour).Unix(),
		OldSn:       nil,
		From:        main.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx := pb.CreateGanyTx(sp, b, nil, nil)
	hash, err := mockBackend.PutBulletin(tx)
	require.NoError(t, err)

	fmt.Printf("hash: %v\n", hash)

	payerNonces, payerBalance, err = mockBackend.LoadWalletInStochasticPay(token, payer)
	require.NoError(t, err)

	fmt.Printf("payerNonces: %v\n", payerNoncesBz)
	fmt.Printf("payerBalance: %v\n", payerBalance.Uint64())
	require.Equal(t, uint64(100-20), payerBalance.Uint64())

	payeeNonces, payeeBalance, err := mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)
	require.Equal(t, uint64(0), payeeBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payeeNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payeeBalance.Uint64())

	validatorNonces, validatorBalance, err := mockBackend.LoadWalletInStochasticPay(token, validator)
	require.NoError(t, err)
	require.Equal(t, uint64(20), validatorBalance.Uint64())
	fmt.Printf("validatorNonces: %v\n", validatorNonces.Bytes32())
	fmt.Printf("validatorBalance: %v\n", validatorBalance.Uint64())
}

// Payee posts a blog.
// Then payer posts a comment for this blog, he/she needs to pay some amount according to condition.
// Then payee posts a comment for his/her own blog, he/she doesn't need to pay any amount for himself/herself.
func TestPutBulletin_Comment(t *testing.T) {
	apps := make([]app.GanyApp, NumOfShards)
	dbs := make([]*badger.DB, NumOfShards)

	for i := 0; i < NumOfShards; i++ {
		dbPath := fmt.Sprintf(TestDataDirTemplate, i)
		db, err := badger.Open(badger.DefaultOptions(dbPath))
		require.NoError(t, err)

		dbs[i] = db
		apps[i] = testutils.CreateMockGanyApp(db)
	}

	defer cleanData(dbs)

	validatorCompressedPubKeys := getCompressedValidatorPubKeys()
	mockClient := testutils.NewMockClient()
	mockFollower := testutils.NewMockFollower(map[gethcmn.Address]gethcmn.Address{
		main:  payer,
		payee: payee,
	}, validatorCompressedPubKeys)
	mockBackend := testutils.NewMockBackend(apps, mockFollower, mockClient, token, contractAddr, validator, validatorPrivateKey)

	delegatedAddr, err := mockBackend.GetDelegatedAddr(main)
	require.NoError(t, err)
	require.Equal(t, payer.Hex(), delegatedAddr.Hex())

	mockFollower.SetWallet(payer, uint256.NewInt(0), uint256.NewInt(100))
	mockFollower.SetWallet(payee, uint256.NewInt(0), uint256.NewInt(100))

	payerNonces, payerBalance, err := mockBackend.LoadWalletInStochasticPay(token, payer)
	payerNoncesBz := payerNonces.Bytes32()
	require.NoError(t, err)

	payeeNonces, payeeBalance, err := mockBackend.LoadWalletInStochasticPay(token, payee)
	payeeNoncesBz := payeeNonces.Bytes32()
	require.NoError(t, err)

	require.Equal(t, uint64(100), payerBalance.Uint64())
	require.Equal(t, uint64(100), payeeBalance.Uint64())

	tree, _ := genMerkleTree(validatorCompressedPubKeys)
	now := time.Now()
	sp1 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             Prob32,
		Nonces:                  payeeNoncesBz[:],
		Payee:                   nil,
		AmountToPayee:           gethcmn.FromHex("0x00"),
		AmountToValidator:       amountToValidator,
		Signature:               make([]byte, 65),
	}

	msg1 := sp1.GenEIP712MsgForAB(token)
	typedData1 := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg1, contractAddr)
	eip712Hash1, err := ethutils.GetTypedDataHash(typedData1)
	require.NoError(t, err)

	sig1, err := ethutils.SignWithEIP712Hash(eip712Hash1, payeePrivateKey)
	require.NoError(t, err)
	copy(sp1.Signature[:], sig1)

	b1 := &pb.Bulletin{
		Type:        pb.Bulletin_BLOG,
		Topic:       []byte{0x12},
		Timestamp:   now.Unix(),
		Duration:    now.Add(time.Hour).Unix(),
		OldSn:       nil,
		From:        payee.Bytes(),
		ContentType: "My Blog",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	staticAddrTree, _ := merkletree.NewTreeWithHashStrategy([]merkletree.Content{cryptoutils.MerkleLeaf{Bz: payee.Bytes()}},
		sha3.NewLegacyKeccak256)

	// Either author himself/herself or eligible for payment
	ac1 := &pb.AuthChallenge{
		DynamicSetChallengeList: nil,
		StaticSetChallengeList: []*pb.StaticSetChallenge{
			{Root: staticAddrTree.MerkleRoot()},
		},
		StochasticPayCondList: []*pb.StochasticPayCondition{
			{
				Payee:       payee.Bytes(),
				Probability: Prob32,
				Amount:      gethcmn.FromHex("0x1E"),
			},
		},
		OrOfAndOfConditions: []*pb.AndOfConditions{
			{ConditionNumbers: nil},
			{ConditionNumbers: []int32{1}},
			{ConditionNumbers: []int32{2}},
		},
	}

	tx1 := pb.CreateGanyTx(sp1, b1, nil, ac1)
	hash1, err := mockBackend.PutBulletin(tx1)
	require.NoError(t, err)
	fmt.Printf("hash1: %v\n", hash1)

	payeeNonces, payeeBalance, err = mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)
	require.Equal(t, uint64(100-20), payeeBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payeeNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payeeBalance.Uint64())

	validatorNonces, validatorBalance, err := mockBackend.LoadWalletInStochasticPay(token, validator)
	require.NoError(t, err)
	require.Equal(t, uint64(20), validatorBalance.Uint64())
	fmt.Printf("validatorNonces: %v\n", validatorNonces.Bytes32())
	fmt.Printf("validatorBalance: %v\n", validatorBalance.Uint64())

	now = time.Now()
	sp2 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             Prob32,
		Nonces:                  payerNoncesBz[:],
		Payee:                   payee.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x1E"),
		AmountToValidator:       amountToValidator,
		Signature:               make([]byte, 65),
	}

	msg2 := sp2.GenEIP712MsgForAB(token)
	typedData2 := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg2, contractAddr)
	eip712Hash2, err := ethutils.GetTypedDataHash(typedData2)
	require.NoError(t, err)

	sig2, err := ethutils.SignWithEIP712Hash(eip712Hash2, payerPrivateKey)
	require.NoError(t, err)
	copy(sp2.Signature[:], sig2)

	var commentTopic []byte
	blogID, err := tx1.GetBulletinID()
	require.NoError(t, err)

	blogTopicHash := b1.GetTopicHash()
	sn := genSerialBytes(b1.Timestamp, 0)

	commentTopic = append(commentTopic, blogID[:]...)
	commentTopic = append(commentTopic, blogTopicHash[:4]...)
	commentTopic = append(commentTopic, sn[:]...)

	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_COMMENT,
		Topic:       commentTopic,
		Timestamp:   now.Unix(),
		Duration:    now.Add(time.Hour).Unix(),
		OldSn:       nil,
		From:        main.Bytes(),
		ContentType: "Comment1 for blog",
		ContentList: [][]byte{
			{3, 4},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	ap2 := &pb.AuthProof{
		DynamicSetProofList: nil,
		StaticSetProofList: []*pb.StaticSetProof{
			{Root: staticAddrTree.MerkleRoot(), Proof: nil},
		},
		StochasticPayCondList: []*pb.StochasticPayCondition{
			{
				Payee:       payee.Bytes(),
				Probability: Prob32,
				Amount:      gethcmn.FromHex("0x1E"),
			},
		},
		OrOfAndOfConditions: []*pb.AndOfConditions{
			{ConditionNumbers: nil},
			{ConditionNumbers: []int32{1}},
			{ConditionNumbers: []int32{2}},
		},
	}

	tx2 := pb.CreateGanyTx(sp2, b2, ap2, nil)
	hash2, err := mockBackend.PutBulletin(tx2)
	require.NoError(t, err)
	fmt.Printf("hash2: %v\n", hash2)

	payerNonces, payerBalance, err = mockBackend.LoadWalletInStochasticPay(token, payer)
	require.NoError(t, err)
	require.Equal(t, uint64(100-20-30), payerBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payerNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payerBalance.Uint64())

	payeeNonces, payeeBalance, err = mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)
	require.Equal(t, uint64(100-20+30), payeeBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payeeNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payeeBalance.Uint64())

	validatorNonces, validatorBalance, err = mockBackend.LoadWalletInStochasticPay(token, validator)
	require.NoError(t, err)
	require.Equal(t, uint64(20+20), validatorBalance.Uint64())
	fmt.Printf("validatorNonces: %v\n", validatorNonces.Bytes32())
	fmt.Printf("validatorBalance: %v\n", validatorBalance.Uint64())

	now = time.Now()
	payeeNoncesBz = payeeNonces.Bytes32()
	sp3 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             Prob32,
		Nonces:                  payeeNoncesBz[:],
		Payee:                   nil,
		AmountToPayee:           gethcmn.FromHex("0x00"),
		AmountToValidator:       amountToValidator,
		Signature:               make([]byte, 65),
	}

	msg3 := sp3.GenEIP712MsgForAB(token)
	typedData3 := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg3, contractAddr)
	eip712Hash3, err := ethutils.GetTypedDataHash(typedData3)
	require.NoError(t, err)

	sig3, err := ethutils.SignWithEIP712Hash(eip712Hash3, payeePrivateKey)
	require.NoError(t, err)
	copy(sp3.Signature[:], sig3)

	b3 := &pb.Bulletin{
		Type:        pb.Bulletin_COMMENT,
		Topic:       commentTopic,
		Timestamp:   now.Unix(),
		Duration:    now.Add(time.Hour).Unix(),
		OldSn:       nil,
		From:        payee.Bytes(),
		ContentType: "Comment2 for blog",
		ContentList: [][]byte{
			{5, 6},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	path, _, _ := staticAddrTree.GetMerklePath(cryptoutils.MerkleLeaf{
		Bz: payee.Bytes(),
	})
	fmt.Printf("proof path: %v\n", path)

	ap3 := &pb.AuthProof{
		DynamicSetProofList: nil,
		StaticSetProofList: []*pb.StaticSetProof{
			{Root: staticAddrTree.MerkleRoot(), Proof: path},
		},
		StochasticPayCondList: []*pb.StochasticPayCondition{
			{
				Payee:       payee.Bytes(),
				Probability: Prob32,
				Amount:      gethcmn.FromHex("0x1E"),
			},
		},
		OrOfAndOfConditions: []*pb.AndOfConditions{
			{ConditionNumbers: nil},
			{ConditionNumbers: []int32{1}},
			{ConditionNumbers: []int32{2}},
		},
	}

	tx3 := pb.CreateGanyTx(sp3, b3, ap3, nil)
	hash3, err := mockBackend.PutBulletin(tx3)
	require.NoError(t, err)
	fmt.Printf("hash3: %v\n", hash3)

	payerNonces, payerBalance, err = mockBackend.LoadWalletInStochasticPay(token, payer)
	require.NoError(t, err)
	require.Equal(t, uint64(100-20-30), payerBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payerNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payerBalance.Uint64())

	payeeNonces, payeeBalance, err = mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)
	require.Equal(t, uint64(100-20+30-20), payeeBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payeeNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payeeBalance.Uint64())

	validatorNonces, validatorBalance, err = mockBackend.LoadWalletInStochasticPay(token, validator)
	require.NoError(t, err)
	require.Equal(t, uint64(20+20+20), validatorBalance.Uint64())
	fmt.Printf("validatorNonces: %v\n", validatorNonces.Bytes32())
	fmt.Printf("validatorBalance: %v\n", validatorBalance.Uint64())
}

// Payer posts a column, which restricts only payer can post
// Then payer & payee post blogs under this column
func TestPutBulletin_Column(t *testing.T) {
	apps := make([]app.GanyApp, NumOfShards)
	dbs := make([]*badger.DB, NumOfShards)

	for i := 0; i < NumOfShards; i++ {
		dbPath := fmt.Sprintf(TestDataDirTemplate, i)
		db, err := badger.Open(badger.DefaultOptions(dbPath))
		require.NoError(t, err)

		dbs[i] = db
		apps[i] = testutils.CreateMockGanyApp(db)
	}

	defer cleanData(dbs)

	validatorCompressedPubKeys := getCompressedValidatorPubKeys()
	mockClient := testutils.NewMockClient()
	mockFollower := testutils.NewMockFollower(map[gethcmn.Address]gethcmn.Address{
		main:  payer,
		payee: payee,
	}, validatorCompressedPubKeys)
	mockBackend := testutils.NewMockBackend(apps, mockFollower, mockClient, token, contractAddr, validator, validatorPrivateKey)

	delegatedAddr, err := mockBackend.GetDelegatedAddr(main)
	require.NoError(t, err)
	require.Equal(t, payer.Hex(), delegatedAddr.Hex())

	mockFollower.SetWallet(payer, uint256.NewInt(0), uint256.NewInt(100))
	mockFollower.SetWallet(payee, uint256.NewInt(0), uint256.NewInt(100))

	payerNonces, payerBalance, err := mockBackend.LoadWalletInStochasticPay(token, payer)

	payerNoncesBz := payerNonces.Bytes32()
	require.NoError(t, err)

	payeeNonces, payeeBalance, err := mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)

	require.Equal(t, uint64(100), payerBalance.Uint64())
	require.Equal(t, uint64(100), payeeBalance.Uint64())

	tree, _ := genMerkleTree(validatorCompressedPubKeys)
	now := time.Now()
	sp1 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             Prob32,
		Nonces:                  payerNoncesBz[:],
		Payee:                   nil,
		AmountToPayee:           gethcmn.FromHex("0x00"),
		AmountToValidator:       amountToValidator,
		Signature:               make([]byte, 65),
	}

	msg1 := sp1.GenEIP712MsgForAB(token)
	typedData1 := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg1, contractAddr)
	eip712Hash1, err := ethutils.GetTypedDataHash(typedData1)
	require.NoError(t, err)

	sig1, err := ethutils.SignWithEIP712Hash(eip712Hash1, payerPrivateKey)
	require.NoError(t, err)
	copy(sp1.Signature[:], sig1)

	staticAddrTree, _ := merkletree.NewTreeWithHashStrategy([]merkletree.Content{cryptoutils.MerkleLeaf{Bz: main.Bytes()}},
		sha3.NewLegacyKeccak256)

	ap1 := &pb.AuthProof{
		DynamicSetProofList: nil,
		StaticSetProofList: []*pb.StaticSetProof{
			{Root: staticAddrTree.MerkleRoot(), Proof: nil},
		},
		StochasticPayCondList: []*pb.StochasticPayCondition{},
		OrOfAndOfConditions: []*pb.AndOfConditions{
			{ConditionNumbers: nil},
			{ConditionNumbers: []int32{1}},
			{ConditionNumbers: nil},
		},
	}

	ac1 := &pb.AuthChallenge{
		DynamicSetChallengeList: nil,
		StaticSetChallengeList: []*pb.StaticSetChallenge{
			{Root: staticAddrTree.MerkleRoot()},
		},
		StochasticPayCondList: nil,
		OrOfAndOfConditions: []*pb.AndOfConditions{
			{ConditionNumbers: nil},
			{ConditionNumbers: []int32{1}},
			{ConditionNumbers: nil},
		},
	}

	ac1Bz, _ := proto.Marshal(ac1)
	ac1Hash := sha256.Sum256(ac1Bz)

	var columnTopic [33]byte
	copy(columnTopic[:32], ac1Hash[:])
	columnTopic[32] = byte(0x12)

	b1 := &pb.Bulletin{
		Type:        pb.Bulletin_COLUMN,
		Topic:       columnTopic[:],
		Timestamp:   now.Unix(),
		Duration:    now.Add(time.Hour).Unix(),
		OldSn:       nil,
		From:        main.Bytes(),
		ContentType: "Column blog 1",
		ContentList: [][]byte{
			{1, 2},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	tx1 := pb.CreateGanyTx(sp1, b1, ap1, ac1)
	hash1, err := mockBackend.PutBulletin(tx1)
	require.NoError(t, err)

	fmt.Printf("hash1: %v\n", hash1)

	payerNonces, payerBalance, err = mockBackend.LoadWalletInStochasticPay(token, payer)
	require.NoError(t, err)
	payerNoncesBz = payerNonces.Bytes32()
	fmt.Printf("payerNonces: %v\n", payerNoncesBz)
	fmt.Printf("payerBalance: %v\n", payerBalance.Uint64())
	require.Equal(t, uint64(100-20), payerBalance.Uint64())

	payeeNonces, payeeBalance, err = mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)
	require.Equal(t, uint64(100), payeeBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payeeNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payeeBalance.Uint64())

	validatorNonces, validatorBalance, err := mockBackend.LoadWalletInStochasticPay(token, validator)
	require.NoError(t, err)
	require.Equal(t, uint64(20), validatorBalance.Uint64())
	fmt.Printf("validatorNonces: %v\n", validatorNonces.Bytes32())
	fmt.Printf("validatorBalance: %v\n", validatorBalance.Uint64())

	now = time.Now()
	sp2 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             Prob32,
		Nonces:                  payerNoncesBz[:],
		Payee:                   nil,
		AmountToPayee:           gethcmn.FromHex("0x00"),
		AmountToValidator:       amountToValidator,
		Signature:               make([]byte, 65),
	}

	msg2 := sp2.GenEIP712MsgForAB(token)
	typedData2 := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg2, contractAddr)
	eip712Hash2, err := ethutils.GetTypedDataHash(typedData2)
	require.NoError(t, err)

	sig2, err := ethutils.SignWithEIP712Hash(eip712Hash2, payerPrivateKey)
	require.NoError(t, err)
	copy(sp2.Signature[:], sig2)

	columnTopic[32] = byte(0x34)
	b2 := &pb.Bulletin{
		Type:        pb.Bulletin_COLUMN,
		Topic:       columnTopic[:],
		Timestamp:   now.Unix(),
		Duration:    now.Add(time.Hour).Unix(),
		OldSn:       nil,
		From:        main.Bytes(),
		ContentType: "Column blog 2",
		ContentList: [][]byte{
			{3, 4},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	ap2 := &pb.AuthProof{
		DynamicSetProofList: nil,
		StaticSetProofList: []*pb.StaticSetProof{
			{Root: staticAddrTree.MerkleRoot(), Proof: nil},
		},
		StochasticPayCondList: []*pb.StochasticPayCondition{},
		OrOfAndOfConditions: []*pb.AndOfConditions{
			{ConditionNumbers: nil},
			{ConditionNumbers: []int32{1}},
			{ConditionNumbers: nil},
		},
	}

	tx2 := pb.CreateGanyTx(sp2, b2, ap2, nil)
	hash2, err := mockBackend.PutBulletin(tx2)
	require.NoError(t, err)
	fmt.Printf("hash2: %v\n", hash2)

	payerNonces, payerBalance, err = mockBackend.LoadWalletInStochasticPay(token, payer)
	require.NoError(t, err)
	require.Equal(t, uint64(100-20-20), payerBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payerNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payerBalance.Uint64())

	payeeNonces, payeeBalance, err = mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)
	payeeNoncesBz := payeeNonces.Bytes32()
	require.Equal(t, uint64(100), payeeBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payeeNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payeeBalance.Uint64())

	validatorNonces, validatorBalance, err = mockBackend.LoadWalletInStochasticPay(token, validator)
	require.NoError(t, err)
	require.Equal(t, uint64(20+20), validatorBalance.Uint64())
	fmt.Printf("validatorNonces: %v\n", validatorNonces.Bytes32())
	fmt.Printf("validatorBalance: %v\n", validatorBalance.Uint64())

	now = time.Now()
	sp3 := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             Prob32,
		Nonces:                  payeeNoncesBz[:],
		Payee:                   nil,
		AmountToPayee:           gethcmn.FromHex("0x00"),
		AmountToValidator:       amountToValidator,
		Signature:               make([]byte, 65),
	}

	msg3 := sp3.GenEIP712MsgForAB(token)
	typedData3 := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg3, contractAddr)
	eip712Hash3, err := ethutils.GetTypedDataHash(typedData3)
	require.NoError(t, err)

	sig3, err := ethutils.SignWithEIP712Hash(eip712Hash3, payeePrivateKey)
	require.NoError(t, err)
	copy(sp3.Signature[:], sig3)

	columnTopic[32] = byte(0x56)
	b3 := &pb.Bulletin{
		Type:        pb.Bulletin_COLUMN,
		Topic:       columnTopic[:],
		Timestamp:   now.Unix(),
		Duration:    now.Add(time.Hour).Unix(),
		OldSn:       nil,
		From:        payee.Bytes(),
		ContentType: "Column blog 3",
		ContentList: [][]byte{
			{5, 6},
		},
		CensoredStart: 0,
		CensoredEnd:   0,
	}

	ap3 := &pb.AuthProof{
		DynamicSetProofList: nil,
		StaticSetProofList: []*pb.StaticSetProof{
			{Root: staticAddrTree.MerkleRoot(), Proof: nil},
		},
		StochasticPayCondList: []*pb.StochasticPayCondition{},
		OrOfAndOfConditions: []*pb.AndOfConditions{
			{ConditionNumbers: nil},
			{ConditionNumbers: []int32{1}},
			{ConditionNumbers: nil},
		},
	}

	tx3 := pb.CreateGanyTx(sp3, b3, ap3, nil)
	_, err = mockBackend.PutBulletin(tx3)
	require.Error(t, err, "incorrect auth proof")

	payerNonces, payerBalance, err = mockBackend.LoadWalletInStochasticPay(token, payer)
	require.NoError(t, err)
	require.Equal(t, uint64(100-20-20), payerBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payerNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payerBalance.Uint64())

	payeeNonces, payeeBalance, err = mockBackend.LoadWalletInStochasticPay(token, payee)
	require.NoError(t, err)
	require.Equal(t, uint64(100), payeeBalance.Uint64())
	fmt.Printf("payeeNonces: %v\n", payeeNonces.Bytes32())
	fmt.Printf("payeeBalance: %v\n", payeeBalance.Uint64())

	validatorNonces, validatorBalance, err = mockBackend.LoadWalletInStochasticPay(token, validator)
	require.NoError(t, err)
	require.Equal(t, uint64(20+20), validatorBalance.Uint64())
	fmt.Printf("validatorNonces: %v\n", validatorNonces.Bytes32())
	fmt.Printf("validatorBalance: %v\n", validatorBalance.Uint64())
}

func cleanData(dbs []*badger.DB) {
	for _, d := range dbs {
		d.DropAll()
		d.Close()
	}

	os.RemoveAll("../testdata")
}
