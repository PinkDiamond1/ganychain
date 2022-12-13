package testutils

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	gethcmn "github.com/ethereum/go-ethereum/common"
	eip712types "github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/golang/protobuf/proto"
	"github.com/holiman/uint256"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/vechain/go-ecvrf"

	"github.com/smartbch/ganychain/app"
	"github.com/smartbch/ganychain/backend"
	"github.com/smartbch/ganychain/follower"
	pb "github.com/smartbch/ganychain/proto"
	"github.com/smartbch/ganychain/utils/ethutils"
	"github.com/smartbch/ganychain/utils/ugo"
	"github.com/smartbch/ganychain/web3client"
)

type MockBackend struct {
	*backend.Backend
	numOfShards uint32
	apps        []app.GanyApp
	follower    *MockFollower

	token         gethcmn.Address
	contractAddr  gethcmn.Address
	validatorAddr gethcmn.Address

	validatorPrivateKey *ecdsa.PrivateKey
}

func NewMockBackend(apps []app.GanyApp, follower follower.FollowerService, sbchClient web3client.Web3Client,
	token, contractAddr, validatorAddr gethcmn.Address, validatorPrivateKey *ecdsa.PrivateKey) *MockBackend {

	be := backend.NewBackend(apps, follower, sbchClient)
	return &MockBackend{
		Backend:             be.(*backend.Backend),
		numOfShards:         uint32(len(apps)),
		apps:                apps,
		follower:            follower.(*MockFollower),
		token:               token,
		contractAddr:        contractAddr,
		validatorAddr:       validatorAddr,
		validatorPrivateKey: validatorPrivateKey,
	}
}

func (m *MockBackend) PutBulletin(tx pb.GanyTx) (tmbytes.HexBytes, error) {
	// 1. check the tx
	if isValid, err := tx.IsValid(); !isValid {
		return nil, err
	}

	// 2. prepare tx data
	sp, err := tx.GetStochasticPayment()
	if err != nil {
		return nil, err
	}

	b, err := tx.GetBulletin()
	if err != nil {
		return nil, err
	}

	ap, err := tx.GetAuthProof()
	if err != nil {
		return nil, err
	}

	msg := sp.GenEIP712MsgForAB(m.token)
	typedData := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg, m.contractAddr)
	eip712Hash, err := ethutils.GetTypedDataHash(typedData)

	// 3. check the address
	address, err := m.checkAndRestoreAddress(sp, b, eip712Hash)
	if err != nil {
		return nil, err
	}

	// 4. check balance and get nonces
	amountToPayee256, amountToValidator256, err := m.checkNoncesAndBalance(sp, *address)
	if err != nil {
		return nil, err
	}

	// 5. check auth
	err = m.checkAuth(sp, b, ap)
	if err != nil {
		return nil, err
	}

	// 6. check probability
	_, err = m.checkProb32(m.validatorPrivateKey, msg, sp.Probability)
	if err != nil {
		return nil, err
	}

	// 7. send to ganychain
	topicHash := b.GetTopicHash()
	shardIndex := binary.BigEndian.Uint32(topicHash[:4]) % m.numOfShards
	commitResult, err := m.apps[shardIndex].BroadcastTx(tx)
	if err != nil {
		return nil, err
	}

	// 8. callPayToAB
	if commitResult.DeliverTx.GetCode() == uint32(0) {
		m.callPayToAB(*address, gethcmn.BytesToAddress(sp.Payee), amountToPayee256, amountToValidator256)
	}

	return commitResult.Hash, nil
}

func (m *MockBackend) checkAndRestoreAddress(sp *pb.StochasticPayment, b *pb.Bulletin, eip712Hash []byte) (*gethcmn.Address, error) {
	address, _, err := ethutils.EcRecover(eip712Hash, sp.Signature)
	if err != nil {
		return nil, err
	}

	mainAddr := gethcmn.BytesToAddress(b.From[:])
	delegatedAddr, err := m.follower.GetDelegatedAddrByMainAddr(mainAddr)
	if err != nil {
		return nil, err
	}

	if delegatedAddr.Hex() != address.Hex() {
		return nil, errors.New("invalid address or delegated address")
	}

	return address, nil
}

func (m *MockBackend) checkNoncesAndBalance(sp *pb.StochasticPayment, address gethcmn.Address) (*uint256.Int, *uint256.Int, error) {
	nonces, balance, err := m.follower.LoadWalletInStochasticPay(m.token, address)
	if err != nil {
		return nil, nil, err
	}

	noncesBz := nonces.Bytes32()
	if !bytes.Equal(noncesBz[:], sp.Nonces[:]) {
		return nil, nil, errors.New("invalid stochastic pay nonces")
	}

	amountToPayee256, overflow := ugo.BytesToUint256(sp.AmountToPayee)
	if overflow {
		return nil, nil, errors.New("invalid amountToPayee")
	}

	amountToValidator256, overflow := ugo.BytesToUint256(sp.AmountToValidator)
	if overflow {
		return nil, nil, errors.New("invalid amountToValidator")
	}

	if balance.Lt(uint256.NewInt(0).Add(amountToPayee256, amountToValidator256)) {
		return nil, nil, errors.New("insufficient balance")
	}

	return amountToPayee256, amountToValidator256, nil
}

func (m *MockBackend) checkProb32(privateKey *ecdsa.PrivateKey, msg eip712types.TypedDataMessage, prob32 uint32) ([]byte, error) {
	payerSalt := msg["payerSalt"].(string)
	alpha, err := uint256.FromHex(payerSalt)
	if err != nil {
		return nil, err
	}

	alphaBytes := alpha.PaddedBytes(32)
	fmt.Printf("alpha: %v\n", alphaBytes)

	betaBytes, pi, err := ecvrf.NewSecp256k1Sha256Tai().Prove(privateKey, alphaBytes)
	rand32 := binary.BigEndian.Uint32(betaBytes)
	fmt.Printf("rand32: %v\n", rand32)

	if prob32 < rand32 {
		return nil, fmt.Errorf("check probability failed: %v < %v", prob32, rand32)
	}
	return pi, nil
}

func (m *MockBackend) checkAuth(sp *pb.StochasticPayment, b *pb.Bulletin, ap *pb.AuthProof) error {
	var sourceAcHash []byte
	needAuth := true

	switch b.GetType() {
	case pb.Bulletin_COMMENT:
		sourceAcHash = b.GetTopic()[32:64]
	case pb.Bulletin_COLUMN:
		sourceAcHash = b.GetTopic()[:32]
	default:
		needAuth = false
	}

	if needAuth {
		ac := ap.GenerateAuthChallenge()
		acBz, err := proto.Marshal(ac)
		if err != nil {
			return err
		}

		acHash := sha256.Sum256(acBz)
		if !bytes.Equal(sourceAcHash[:], acHash[:]) {
			return errors.New("incorrect auth challenge hash")
		}

		if !ap.CheckAuthProof(sp, b) {
			return errors.New("incorrect auth proof")
		}
	}

	return nil
}

func (m *MockBackend) callPayToAB(payer, payee gethcmn.Address, amountToPayee256, amountToValidator256 *uint256.Int) {
	m.updateWallet(payer, uint256.NewInt(0).Add(amountToPayee256, amountToValidator256), -1)
	if amountToPayee256.Gt(uint256.NewInt(0)) {
		m.updateWallet(payee, amountToPayee256, 1)
	}
	m.updateWallet(m.validatorAddr, amountToValidator256, 1)
}

func (m *MockBackend) updateWallet(owner gethcmn.Address, delta *uint256.Int, sign int) {
	nonces, balance, _ := m.follower.LoadWalletInStochasticPay(gethcmn.Address{}, owner)
	noncesBz := nonces.Bytes32()

	var newBalance *uint256.Int
	var newNonces *uint256.Int

	if sign > 0 {
		// receive
		newBalance = uint256.NewInt(0).Add(balance, delta)
		newNonces = nonces
	} else {
		// receive
		newBalance = uint256.NewInt(0).Sub(balance, delta)
		newNoncesBz := addNonces(noncesBz)
		newNonces = uint256.NewInt(0).SetBytes32(newNoncesBz[:])
	}

	m.follower.SetWallet(owner, newNonces, newBalance)
}

func addNonces(noncesBz [32]byte) [32]byte {
	var result [32]byte
	copy(result[:], noncesBz[:])
	result[31]++
	result[27]++
	result[23]++
	return result
}
