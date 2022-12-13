package backend

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	eip712types "github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/golang/protobuf/proto"
	"github.com/holiman/uint256"
	"github.com/smartbch/merkletree"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/vechain/go-ecvrf"
	"golang.org/x/crypto/sha3"

	"github.com/smartbch/ganychain/app"
	"github.com/smartbch/ganychain/contract"
	"github.com/smartbch/ganychain/follower"
	pb "github.com/smartbch/ganychain/proto"
	"github.com/smartbch/ganychain/utils/cryptoutils"
	"github.com/smartbch/ganychain/utils/ethutils"
	"github.com/smartbch/ganychain/utils/ugo"
	"github.com/smartbch/ganychain/web3client"
)

var (
	// FIXME: get validator private key in a safe way
	validatorPrivateKey, _ = gethcrypto.HexToECDSA("5f41e5ff714e6e9df08fefd36931b908c04b4fd6d90d70223abd85128f56afb9") // local bch test key
)

var _ BackendService = &Backend{}

type Backend struct {
	numOfShards uint32
	apps        []app.GanyApp // gany applications
	follower    follower.FollowerService
	sbchClient  web3client.Web3Client
}

func NewBackend(apps []app.GanyApp, follower follower.FollowerService, sbchClient web3client.Web3Client) BackendService {
	return &Backend{
		numOfShards: uint32(len(apps)),
		apps:        apps,
		follower:    follower,
		sbchClient:  sbchClient,
	}
}

func (backend *Backend) GetAllChainIds() []string {
	chainIds := make([]string, 0, backend.numOfShards)
	for _, a := range backend.apps {
		chainIds = append(chainIds, a.GetChainId())
	}
	return chainIds
}

func (backend *Backend) PutBulletin(tx pb.GanyTx) (tmbytes.HexBytes, error) {
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

	msg := sp.GenEIP712MsgForAB(contract.SBCHTokenAddress)
	typedData := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg, contract.StochasticPayVRFAddress)
	eip712Hash, err := ethutils.GetTypedDataHash(typedData)

	// 3. check the address
	address, err := backend.checkAndRestoreAddress(sp, b, eip712Hash)
	if err != nil {
		return nil, err
	}

	// 4. check balance and get nonces
	_, _, err = backend.checkNoncesAndBalance(sp, *address)
	if err != nil {
		return nil, err
	}

	// 5. check auth
	err = backend.checkAuth(sp, b, ap)
	if err != nil {
		return nil, err
	}

	// 5. check probability
	pi, err := backend.checkProb32(validatorPrivateKey, msg, sp.Probability)
	if err != nil {
		return nil, err
	}

	// 7. send to ganychain
	topicHash := b.GetTopicHash()
	shardIndex := binary.BigEndian.Uint32(topicHash[:4]) % backend.numOfShards

	commitResult, err := backend.apps[shardIndex].BroadcastTx(tx)
	if err != nil {
		return nil, err
	}

	if commitResult.CheckTx.GetCode() != 0 {
		return nil, fmt.Errorf("code: %v, error: %v", commitResult.CheckTx.GetCode(), commitResult.CheckTx.GetLog())
	}

	if commitResult.DeliverTx.GetCode() != 0 {
		return nil, fmt.Errorf("code: %v, error: %v", commitResult.DeliverTx.GetCode(), commitResult.DeliverTx.GetLog())
	}

	// 8. call payToAB
	stochasticPay, err := contract.NewStochasticPayVRF(contract.StochasticPayVRFAddress, backend.sbchClient)
	if err != nil {
		return nil, err
	}

	auth, err := bind.NewKeyedTransactorWithChainID(validatorPrivateKey, big.NewInt(10001))
	if err != nil {
		return nil, err
	}
	auth.GasLimit = 8000000
	auth.GasPrice = big.NewInt(10000000000)

	r, s, v := sp.GetRSV()
	payABTx, err := backend.callPayToAB(stochasticPay, auth, msg, r, s, v, pi)
	if err != nil {
		return nil, err
	}

	fmt.Printf("payABTx: %v\n", payABTx.Hash())

	// 9. check transaction receipt
	timeoutCtx, _ := context.WithTimeout(context.Background(), 20*time.Second)
	err = ugo.Retry(timeoutCtx, "Check TxReceipt", 4e3, func() error {
		payABTxReceipt, err := backend.sbchClient.TransactionReceipt(context.Background(), payABTx.Hash())
		if err != nil {
			return err
		}

		fmt.Printf("payABTx Receipt: %+v\n", payABTxReceipt)

		if payABTxReceipt.Status != gethtypes.ReceiptStatusSuccessful {
			return fmt.Errorf("payToAB is not successful: %v", payABTxReceipt.Status)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return commitResult.Hash, nil
}

func (backend *Backend) callPayToAB(stochasticPay *contract.StochasticPayVRF, auth *bind.TransactOpts,
	msg eip712types.TypedDataMessage, r, s [32]byte, v byte, pi []byte) (*gethtypes.Transaction, error) {

	payerSalt := msg["payerSalt"].(string)
	pkHashRoot := msg["pkHashRoot"].(string)
	sep20ContractDueTime64Prob32 := msg["sep20Contract_dueTime64_prob32"].(string)
	seenNonces := msg["seenNonces"].(string)
	payeeAddrAAmountA := msg["payeeAddrA_amountA"].(string)
	amountB := msg["amountB"].(string)

	payerSaltBigInt, _ := new(big.Int).SetString(payerSalt, 0)
	pkHashRootBz, _ := hexutil.Decode(pkHashRoot)
	var pkHashRootBz32 [32]byte
	copy(pkHashRootBz32[:], pkHashRootBz)
	sep20ContractDueTime64Prob32BigInt, _ := new(big.Int).SetString(sep20ContractDueTime64Prob32, 0)
	seenNoncesBigInt, _ := new(big.Int).SetString(seenNonces, 0)
	payeeAddrAAmountABigInt, _ := new(big.Int).SetString(payeeAddrAAmountA, 0)
	amountBBz, _ := hexutil.Decode(amountB)
	amountBVBz := append(amountBBz, v)
	amountBVBzBigInt, _ := new(big.Int).SetString(hexutil.Encode(amountBVBz), 0)

	// TODO: get validator public key in a safe way
	validatorPubKeyXY, err := cryptoutils.PublicKeyToXY(validatorPrivateKey.PublicKey)
	xBigInt, _ := new(big.Int).SetString(hexutil.Encode(validatorPubKeyXY[:32]), 0)
	yBigInt, _ := new(big.Int).SetString(hexutil.Encode(validatorPubKeyXY[32:]), 0)

	param := contract.StochasticPayVRFParams{
		PayerSalt:                    payerSaltBigInt,
		PkX:                          xBigInt,
		PkY:                          yBigInt,
		PkHashRoot:                   pkHashRootBz32,
		Sep20ContractDueTime64Prob32: sep20ContractDueTime64Prob32BigInt,
		SeenNonces:                   seenNoncesBigInt,
		PayeeAddrAAmountA:            payeeAddrAAmountABigInt,
		AmountBV:                     amountBVBzBigInt,
		R:                            r,
		S:                            s,
	}

	tree, err := backend.genValidatorPubKeysMerkleTree()
	if err != nil {
		return nil, err
	}

	proof, err := cryptoutils.GetProof(tree, validatorPubKeyXY)
	if err != nil {
		return nil, err
	}

	payABTx, err := stochasticPay.PayToAB(auth, proof, pi, param)
	if err != nil {
		return nil, err
	}
	return payABTx, nil
}

func (backend *Backend) genValidatorPubKeysMerkleTree() (*merkletree.MerkleTree, error) {
	validatorPubKeys := backend.follower.GetValidatorPubKeyList()

	leaves := make([]merkletree.Content, 0, len(validatorPubKeys))
	for _, cpk := range validatorPubKeys {
		xy, err := cryptoutils.CompressedPubKeyToXY(cpk)
		if err != nil {
			return nil, err
		}

		leaves = append(leaves, cryptoutils.MerkleLeaf{Bz: xy})
	}
	return merkletree.NewTreeWithHashStrategy(leaves, sha3.NewLegacyKeccak256)
}

func (backend *Backend) checkAndRestoreAddress(sp *pb.StochasticPayment, b *pb.Bulletin, eip712Hash []byte) (*gethcmn.Address, error) {
	address, _, err := ethutils.EcRecover(eip712Hash, sp.Signature)
	if err != nil {
		return nil, err
	}

	mainAddr := gethcmn.BytesToAddress(b.From[:])
	delegatedAddr, err := backend.follower.GetDelegatedAddrByMainAddr(mainAddr)
	if err != nil {
		return nil, err
	}

	if delegatedAddr.Hex() != address.Hex() {
		return nil, errors.New("invalid address or delegated address")
	}

	return address, nil
}

func (backend *Backend) checkNoncesAndBalance(sp *pb.StochasticPayment, address gethcmn.Address) (*uint256.Int, *uint256.Int, error) {
	nonces, balance, err := backend.follower.LoadWalletInStochasticPay(contract.SBCHTokenAddress, address)
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

func (backend *Backend) checkProb32(privateKey *ecdsa.PrivateKey, msg eip712types.TypedDataMessage, prob32 uint32) ([]byte, error) {
	payerSalt := msg["payerSalt"].(string)
	alpha, err := uint256.FromHex(payerSalt)
	if err != nil {
		return nil, err
	}

	alphaBytes := alpha.PaddedBytes(32)
	fmt.Printf("alpha: %v\n", alphaBytes)

	betaBytes, pi, err := ecvrf.NewSecp256k1Sha256Tai().Prove(privateKey, alphaBytes)
	if len(betaBytes) != 32 {
		return nil, errors.New("invalid VRF beta bytes")
	}

	var rand32Bz [4]byte
	rand32Bz[0] = betaBytes[3]
	rand32Bz[1] = betaBytes[2]
	rand32Bz[2] = betaBytes[1]
	rand32Bz[3] = betaBytes[0]
	rand32 := binary.BigEndian.Uint32(rand32Bz[:])
	fmt.Printf("rand32: %v\n", rand32)

	if prob32 < rand32 {
		return nil, fmt.Errorf("check probability failed: %v < %v", prob32, rand32)
	}
	return pi, nil
}

func (backend *Backend) checkAuth(sp *pb.StochasticPayment, b *pb.Bulletin, ap *pb.AuthProof) error {
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

// ----------------------------------------------------------------

func (backend *Backend) GetBulletinByGanyUrl(ganyUrlBz []byte) (*pb.Bulletin, error) {
	tx, err := backend.GetGanyTxByGanyUrl(ganyUrlBz)
	if err != nil {
		return nil, err
	}
	return tx.GetBulletin()
}

func (backend *Backend) GetGanyTxByGanyUrl(ganyUrlBz []byte) (pb.GanyTx, error) {
	shardIndex := binary.BigEndian.Uint32(ganyUrlBz[:4]) % backend.numOfShards
	return backend.apps[shardIndex].GetGanyTxByUrl(ganyUrlBz)
}

func (backend *Backend) QueryBulletinByTimePeriod(typ pb.Bulletin_BulletinType, topicHash [32]byte, start, end int64,
	excludeSNs map[string]struct{}) ([]*pb.Bulletin, error) {

	shardIndex := binary.BigEndian.Uint32(topicHash[:4]) % backend.numOfShards
	return backend.apps[shardIndex].QueryBulletinByTimePeriod(typ, topicHash, start, end, excludeSNs)
}

// ----------------------------------------------------------------

func (backend *Backend) GetDelegatedAddr(mainAddress gethcmn.Address) (gethcmn.Address, error) {
	return backend.follower.GetDelegatedAddrByMainAddr(mainAddress)
}

func (backend *Backend) LoadWalletInStochasticPay(tokenAddr, ownerAddr gethcmn.Address) (*uint256.Int, *uint256.Int, error) {
	return backend.follower.LoadWalletInStochasticPay(tokenAddr, ownerAddr)
}

func (backend *Backend) GetValidatorPubKeyList() [][]byte {
	return backend.follower.GetValidatorPubKeyList()
}
