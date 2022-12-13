package contract

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/smartbch/merkletree"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/sha3"

	pb "github.com/smartbch/ganychain/proto"
	"github.com/smartbch/ganychain/utils/cryptoutils"
	"github.com/smartbch/ganychain/utils/ethutils"
)

const (
	SimulatedChainId = 1337
)

var (
	validator = gethcmn.HexToAddress("0x423403784Ca5bD868731d604Ad097f126B36CAe2")
	main      = gethcmn.HexToAddress("0x06C14ED469FB93545cbF071b593D8f90194Ede62")
	payer     = gethcmn.HexToAddress("0x6f36Cf5520b10F77a92a72276983d9f0E6327E31")
	payee     = gethcmn.HexToAddress("0xE0F007dab8543052dfc4C23Cf8a3aDb848A875f9")

	validatorPrivateKey, _ = gethcrypto.HexToECDSA("5f41e5ff714e6e9df08fefd36931b908c04b4fd6d90d70223abd85128f56afb9") // local bch test key
	mainPrivateKey, _      = gethcrypto.HexToECDSA("85d8e1398312704c5edff03f626d00556620d041d3f86bab2e943a7fe2b31611") // local bch test key
	payerPrivateKey, _     = gethcrypto.HexToECDSA("0eec7081343dba52a5e117f82f58fe7a2bc1fd156e1e858ff52aea5b121746cd") // local bch test key
	payeePrivateKey, _     = gethcrypto.HexToECDSA("ad3bbcddb9818be5eef2e22c28aaa8c7034cc610a8ad9cff1751201a4e2c25eb") // local bch test key
)

func genMerkleTree() *merkletree.MerkleTree {
	validatorPubKeyXY, _ := cryptoutils.PublicKeyToXY(validatorPrivateKey.PublicKey)
	mainPubKeyXY, _ := cryptoutils.PublicKeyToXY(mainPrivateKey.PublicKey)
	payerPubKeyXY, _ := cryptoutils.PublicKeyToXY(payerPrivateKey.PublicKey)
	payeePubKeyXY, _ := cryptoutils.PublicKeyToXY(payeePrivateKey.PublicKey)

	var leaves []merkletree.Content
	leaves = append(leaves, cryptoutils.MerkleLeaf{Bz: validatorPubKeyXY})
	leaves = append(leaves, cryptoutils.MerkleLeaf{Bz: mainPubKeyXY})
	leaves = append(leaves, cryptoutils.MerkleLeaf{Bz: payerPubKeyXY})
	leaves = append(leaves, cryptoutils.MerkleLeaf{Bz: payeePubKeyXY})
	tree, _ := merkletree.NewTreeWithHashStrategy(leaves, sha3.NewLegacyKeccak256)
	return tree
}

func getProof(tree *merkletree.MerkleTree, bz []byte) [][32]byte {
	var result [][32]byte
	c := cryptoutils.MerkleLeaf{Bz: bz}
	path, _, _ := tree.GetMerklePath(c)
	if len(path) == 0 {
		return result
	}

	result = make([][32]byte, len(path))
	for i, p := range path {
		copy(result[i][:], p)
	}

	return result
}

func TestPayToSingleReceiver(t *testing.T) {
	// gen peyer account
	auth, _ := bind.NewKeyedTransactorWithChainID(mainPrivateKey, big.NewInt(SimulatedChainId))

	// setup simulated blockchain
	auth.GasLimit = 8000000
	auth.GasPrice = big.NewInt(10000000000)

	genAlloc := make(core.GenesisAlloc)
	genAlloc[auth.From] = core.GenesisAccount{Balance: big.NewInt(1e18)}

	sim := backends.NewSimulatedBackend(genAlloc, 8000000)
	defer sim.Close()

	// deploy token and stochasticPayVrf contract
	tokenAddr, _, myToken, err := DeployMyToken(auth, sim, "MYT", big.NewInt(100000000), 8)
	require.NoError(t, err)
	sim.Commit()

	contractAddr, _, stochasticPay, err := DeployStochasticPayVRFUT(auth, sim)
	require.NoError(t, err)
	sim.Commit()

	fmt.Printf("token address: %v\n", tokenAddr)
	fmt.Printf("contract address: %v\n", contractAddr)

	// approve token to stochasticPayVrf contract
	_, err = myToken.Approve(auth, contractAddr, big.NewInt(100))
	require.NoError(t, err)
	sim.Commit()

	var payerKeyBz [64]byte
	copy(payerKeyBz[12:32], tokenAddr[:])
	copy(payerKeyBz[44:64], payer[:])
	loadWalletRes, err := stochasticPay.LoadWallet(nil, payerKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(0), loadWalletRes.Nonces.Uint64())
	require.Equal(t, uint64(0), loadWalletRes.Balance.Uint64())

	var tokenAddrAmount [32]byte
	copy(tokenAddrAmount[:20], tokenAddr[:])
	tokenAddrAmount[31] = byte(100)
	tokenAmountBigInt := uint256.NewInt(0).SetBytes32(tokenAddrAmount[:])

	// deposit token to payer
	depositTx, err := stochasticPay.Deposit(auth, payer, tokenAmountBigInt.ToBig())
	require.NoError(t, err)
	sim.Commit()

	depositReceipt, err := sim.TransactionReceipt(context.Background(), depositTx.Hash())
	require.NoError(t, err)
	require.Equal(t, uint64(1), depositReceipt.Status)

	loadWalletRes2, err := stochasticPay.LoadWallet(nil, payerKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(0), loadWalletRes2.Nonces.Uint64())
	require.Equal(t, uint64(100), loadWalletRes2.Balance.Uint64())

	nonces, _ := uint256.FromBig(loadWalletRes2.Nonces)
	noncesBz := nonces.Bytes32()

	tree := genMerkleTree()

	now := time.Now()
	sp := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             1000,
		Nonces:                  noncesBz[:],
		Payee:                   payee.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"),
		AmountToValidator:       gethcmn.FromHex("0x00"),
		Signature:               make([]byte, 65),
	}

	msg := sp.GenEIP712MsgForSR(tokenAddr)
	typedData := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForSR, msg, contractAddr)
	eip712Hash, err := ethutils.GetTypedDataHash(typedData)
	require.NoError(t, err)

	sig, err := ethutils.SignWithEIP712Hash(eip712Hash, payerPrivateKey)
	require.NoError(t, err)

	copy(sp.Signature[:], sig)

	pi := []byte{0x00}
	payerSaltPk0 := msg["payerSalt_pk0"].(string)
	pkTail := msg["pkTail"].(string)
	payeeAddrDueTime64Prob32 := msg["payeeAddr_dueTime64_prob32"].(string)
	seenNonces := msg["seenNonces"].(string)
	sep20ContractAmount := msg["sep20Contract_amount"].(string)
	r, s, v := sp.GetRSV()

	var payerSaltPk0VBz [32]byte
	payerSaltPk0Bz, _ := hexutil.Decode(payerSaltPk0)
	copy(payerSaltPk0VBz[:31], payerSaltPk0Bz[:31])
	payerSaltPk0VBz[31] = v

	payerSaltPk0V := hexutil.Encode(payerSaltPk0VBz[:])
	payerSaltPk0VBigInt, _ := new(big.Int).SetString(payerSaltPk0V, 0)
	payerSaltPk0BigInt, _ := new(big.Int).SetString(payerSaltPk0, 0)
	pkTailBigInt, _ := new(big.Int).SetString(pkTail, 0)
	payeeAddrDueTime64Prob32BigInt, _ := new(big.Int).SetString(payeeAddrDueTime64Prob32, 0)
	seenNoncesBigInt, _ := new(big.Int).SetString(seenNonces, 0)
	sep20ContractAmountBigInt, _ := new(big.Int).SetString(sep20ContractAmount, 0)

	fmt.Printf("payerSaltPk0: %v\n", payerSaltPk0)
	fmt.Printf("payerSaltPk0V: %v\n", payerSaltPk0V)
	fmt.Printf("pkTail: %v\n", pkTail)
	fmt.Printf("payeeAddrDueTime64Prob32: %v\n", payeeAddrDueTime64Prob32)
	fmt.Printf("seenNonces: %v\n", seenNonces)
	fmt.Printf("sep20ContractAmount: %v\n", sep20ContractAmount)

	param := StochasticPayVRFParamsSr{
		PayerSaltPk0V:            payerSaltPk0VBigInt,
		PkTail:                   pkTailBigInt,
		PayeeAddrDueTime64Prob32: payeeAddrDueTime64Prob32BigInt,
		SeenNonces:               seenNoncesBigInt,
		Sep20ContractAmount:      sep20ContractAmountBigInt,
		R:                        r,
		S:                        s,
	}

	// check eip712Hash
	eip712HashFromSbch, err := stochasticPay.GetEIP712HashSr(nil,
		payerSaltPk0BigInt, pkTailBigInt, payeeAddrDueTime64Prob32BigInt, seenNoncesBigInt, sep20ContractAmountBigInt)
	require.NoError(t, err)
	require.EqualValues(t, eip712Hash, eip712HashFromSbch[:])

	// check payer address
	payerAddrFromSbch, err := stochasticPay.GetPayerSr(nil,
		payerSaltPk0VBigInt, pkTailBigInt, payeeAddrDueTime64Prob32BigInt, seenNoncesBigInt, sep20ContractAmountBigInt, r, s)
	require.NoError(t, err)
	require.EqualValues(t, payer, payerAddrFromSbch)

	// payer -> payee 80 MYT
	paySRTx, err := stochasticPay.PayToSingleReciever(auth, pi, param)
	require.NoError(t, err)
	sim.Commit()

	paySRTxReceipt, err := sim.TransactionReceipt(context.Background(), paySRTx.Hash())
	require.NoError(t, err)
	require.Equal(t, uint64(1), paySRTxReceipt.Status)

	// after payment, check balance
	loadWalletRes3, err := stochasticPay.LoadWallet(nil, payerKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(20), loadWalletRes3.Balance.Uint64())

	payerNonces, _ := uint256.FromBig(loadWalletRes3.Nonces)
	fmt.Printf("payer nonces: %v\n", payerNonces.Bytes32())

	var payeeKeyBz [64]byte
	copy(payeeKeyBz[12:32], tokenAddr[:])
	copy(payeeKeyBz[44:64], payee[:])
	loadWalletRes4, err := stochasticPay.LoadWallet(nil, payeeKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(80), loadWalletRes4.Balance.Uint64())

	payeeNonces, _ := uint256.FromBig(loadWalletRes4.Nonces)
	fmt.Printf("payee nonces: %v\n", payeeNonces.Bytes32())
}

func TestPayToAB(t *testing.T) {
	// gen peyer account
	auth, _ := bind.NewKeyedTransactorWithChainID(mainPrivateKey, big.NewInt(SimulatedChainId))

	// setup simulated blockchain
	auth.GasLimit = 8000000
	auth.GasPrice = big.NewInt(10000000000)

	genAlloc := make(core.GenesisAlloc)
	genAlloc[auth.From] = core.GenesisAccount{Balance: big.NewInt(1e18)}

	sim := backends.NewSimulatedBackend(genAlloc, 8000000)
	defer sim.Close()

	// deploy token and stochasticPayVrf contract
	tokenAddr, _, myToken, err := DeployMyToken(auth, sim, "MYT", big.NewInt(100000000), 8)
	require.NoError(t, err)
	sim.Commit()

	contractAddr, _, stochasticPay, err := DeployStochasticPayVRFUT(auth, sim)
	require.NoError(t, err)
	sim.Commit()

	fmt.Printf("token address: %v\n", tokenAddr)
	fmt.Printf("contract address: %v\n", contractAddr)

	// approve token to stochasticPayVrf contract
	_, err = myToken.Approve(auth, contractAddr, big.NewInt(100))
	require.NoError(t, err)
	sim.Commit()

	var payerKeyBz [64]byte
	copy(payerKeyBz[12:32], tokenAddr[:])
	copy(payerKeyBz[44:64], payer[:])
	loadWalletRes, err := stochasticPay.LoadWallet(nil, payerKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(0), loadWalletRes.Nonces.Uint64())
	require.Equal(t, uint64(0), loadWalletRes.Balance.Uint64())

	var tokenAddrAmount [32]byte
	copy(tokenAddrAmount[:20], tokenAddr[:])
	tokenAddrAmount[31] = byte(100)
	tokenAmountBigInt := uint256.NewInt(0).SetBytes32(tokenAddrAmount[:])

	// deposit token to payer
	depositTx, err := stochasticPay.Deposit(auth, payer, tokenAmountBigInt.ToBig())
	require.NoError(t, err)
	sim.Commit()

	depositReceipt, err := sim.TransactionReceipt(context.Background(), depositTx.Hash())
	require.NoError(t, err)
	require.Equal(t, uint64(1), depositReceipt.Status)

	loadWalletRes2, err := stochasticPay.LoadWallet(nil, payerKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(0), loadWalletRes2.Nonces.Uint64())
	require.Equal(t, uint64(100), loadWalletRes2.Balance.Uint64())

	nonces, _ := uint256.FromBig(loadWalletRes2.Nonces)
	noncesBz := nonces.Bytes32()

	tree := genMerkleTree()
	now := time.Now()
	sp := &pb.StochasticPayment{
		ValidatorPubkeyHashRoot: tree.MerkleRoot(),
		DueTime:                 now.Add(time.Hour).Unix(),
		Probability:             1000,
		Nonces:                  noncesBz[:],
		Payee:                   payee.Bytes(),
		AmountToPayee:           gethcmn.FromHex("0x50"), // 80
		AmountToValidator:       gethcmn.FromHex("0x14"), // 20
		Signature:               make([]byte, 65),
	}

	msg := sp.GenEIP712MsgForAB(tokenAddr)
	typedData := ethutils.GetStochasticPayTypedData(ethutils.EIP712TypesForAB, msg, contractAddr)
	eip712Hash, err := ethutils.GetTypedDataHash(typedData)
	require.NoError(t, err)

	sig, err := ethutils.SignWithEIP712Hash(eip712Hash, payerPrivateKey)
	require.NoError(t, err)

	copy(sp.Signature[:], sig)

	pi := []byte{0x00}
	payerSalt := msg["payerSalt"].(string)
	pkHashRoot := msg["pkHashRoot"].(string)
	sep20ContractDueTime64Prob32 := msg["sep20Contract_dueTime64_prob32"].(string)
	seenNonces := msg["seenNonces"].(string)
	payeeAddrAAmountA := msg["payeeAddrA_amountA"].(string)
	amountB := msg["amountB"].(string)
	r, s, v := sp.GetRSV()

	payerSaltBigInt, _ := new(big.Int).SetString(payerSalt, 0)
	pkHashRootBz, _ := hexutil.Decode(pkHashRoot)
	var pkHashRootBz32 [32]byte
	copy(pkHashRootBz32[:], pkHashRootBz)
	sep20ContractDueTime64Prob32BigInt, _ := new(big.Int).SetString(sep20ContractDueTime64Prob32, 0)
	seenNoncesBigInt, _ := new(big.Int).SetString(seenNonces, 0)
	payeeAddrAAmountABigInt, _ := new(big.Int).SetString(payeeAddrAAmountA, 0)
	amountBBz, _ := hexutil.Decode(amountB)
	amountBBigInt, _ := new(big.Int).SetString(amountB, 0)
	amountBVBz := append(amountBBz, v)
	amountBVBzBigInt, _ := new(big.Int).SetString(hexutil.Encode(amountBVBz), 0)

	xy, err := cryptoutils.PublicKeyToXY(validatorPrivateKey.PublicKey)
	require.NoError(t, err)
	xBigInt, _ := new(big.Int).SetString(hexutil.Encode(xy[:32]), 0)
	yBigInt, _ := new(big.Int).SetString(hexutil.Encode(xy[32:]), 0)

	param := StochasticPayVRFParams{
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

	// check eip712Hash
	eip712HashFromSbch, err := stochasticPay.GetEIP712HashAb(nil,
		payerSaltBigInt, pkHashRootBz32, sep20ContractDueTime64Prob32BigInt, seenNoncesBigInt, payeeAddrAAmountABigInt, amountBBigInt)
	require.NoError(t, err)
	require.EqualValues(t, eip712Hash, eip712HashFromSbch[:])

	// check payer address
	payerAddrFromSbch, err := stochasticPay.GetPayerAb(nil,
		payerSaltBigInt, pkHashRootBz32, sep20ContractDueTime64Prob32BigInt, seenNoncesBigInt, payeeAddrAAmountABigInt, amountBBigInt, v, r, s)
	require.NoError(t, err)
	require.EqualValues(t, payer, payerAddrFromSbch)

	proof := getProof(tree, xy)
	require.Len(t, proof, 2)
	for i, p := range proof {
		fmt.Printf("proof %d: %v\n", i, hexutil.Encode(p[:]))
	}

	// payer -> payee 80 MYT
	// payer -> validator 20 MYT
	payABTx, err := stochasticPay.PayToAB(auth, proof, pi, param)
	require.NoError(t, err)
	sim.Commit()

	payABTxReceipt, err := sim.TransactionReceipt(context.Background(), payABTx.Hash())
	require.NoError(t, err)
	require.Equal(t, uint64(1), payABTxReceipt.Status)

	// after payment, check balance
	loadWalletRes3, err := stochasticPay.LoadWallet(nil, payerKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(0), loadWalletRes3.Balance.Uint64())

	payerNonces, _ := uint256.FromBig(loadWalletRes3.Nonces)
	fmt.Printf("payer nonces: %v\n", payerNonces.Bytes32())

	var payeeKeyBz [64]byte
	copy(payeeKeyBz[12:32], tokenAddr[:])
	copy(payeeKeyBz[44:64], payee[:])
	loadWalletRes4, err := stochasticPay.LoadWallet(nil, payeeKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(80), loadWalletRes4.Balance.Uint64())

	payeeNonces, _ := uint256.FromBig(loadWalletRes4.Nonces)
	fmt.Printf("payee nonces: %v\n", payeeNonces.Bytes32())

	var validatorAddrKeyBz [64]byte
	copy(validatorAddrKeyBz[12:32], tokenAddr[:])
	copy(validatorAddrKeyBz[44:64], validator[:])
	loadWalletRes5, err := stochasticPay.LoadWallet(nil, validatorAddrKeyBz[:])
	require.NoError(t, err)
	require.Equal(t, uint64(20), loadWalletRes5.Balance.Uint64())

	vaNonces, _ := uint256.FromBig(loadWalletRes5.Nonces)
	fmt.Printf("validator nonces: %v\n", vaNonces.Bytes32())
}
