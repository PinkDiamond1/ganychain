package ethutils

import (
	"crypto/ecdsa"
	"fmt"

	gethcmn "github.com/ethereum/go-ethereum/common"
	gethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	eip712types "github.com/ethereum/go-ethereum/signer/core/apitypes"
)

var (
	EIP712TypesForSR = []eip712types.Type{
		{Name: "payerSalt_pk0", Type: "uint256"},
		{Name: "pkTail", Type: "uint256"},
		{Name: "payeeAddr_dueTime64_prob32", Type: "uint256"},
		{Name: "seenNonces", Type: "uint256"},
		{Name: "sep20Contract_amount", Type: "uint256"},
	}

	EIP712TypesForAB = []eip712types.Type{
		{Name: "payerSalt", Type: "uint256"},
		{Name: "pkHashRoot", Type: "bytes32"},
		{Name: "sep20Contract_dueTime64_prob32", Type: "uint256"},
		{Name: "seenNonces", Type: "uint256"},
		{Name: "payeeAddrA_amountA", Type: "uint256"},
		{Name: "amountB", Type: "uint256"},
	}
)

func GetStochasticPayTypedData(eip712Types []eip712types.Type, eip712Msg eip712types.TypedDataMessage,
	contractAddr gethcmn.Address) eip712types.TypedData {

	return eip712types.TypedData{
		Types: eip712types.Types{
			"EIP712Domain": []eip712types.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
				{Name: "salt", Type: "bytes32"},
			},
			"Pay": eip712Types,
		},
		PrimaryType: "Pay",
		Domain: eip712types.TypedDataDomain{
			Name:              "stochastic_payment",
			Version:           "v0.1.0",
			ChainId:           gethmath.NewHexOrDecimal256(10000),
			VerifyingContract: contractAddr.Hex(),
			Salt:              crypto.Keccak256Hash([]byte("StochasticPay_VRF")).Hex(),
		},
		Message: eip712Msg,
	}
}

func GetTypedDataHash(typedData eip712types.TypedData) ([]byte, error) {
	eip712Hash, _, err := eip712types.TypedDataAndHash(typedData)
	if err != nil {
		return nil, err
	}
	return eip712Hash, nil
}

func EcRecover(eip712Hash, sig []byte) (*gethcmn.Address, *ecdsa.PublicKey, error) {
	if len(sig) != 65 {
		return nil, nil, fmt.Errorf("signature must be 65 bytes long")
	}
	if sig[64] != 27 && sig[64] != 28 {
		return nil, nil, fmt.Errorf("invalid signature (V is not 27 or 28)")
	}

	var rsvSig [65]byte
	copy(rsvSig[:], sig)
	rsvSig[64] -= 27

	pubKeyRaw, err := crypto.Ecrecover(eip712Hash, rsvSig[:])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid signature: %s", err.Error())
	}

	pubKey, err := crypto.UnmarshalPubkey(pubKeyRaw)
	if err != nil {
		return nil, nil, err
	}

	address := crypto.PubkeyToAddress(*pubKey)
	return &address, pubKey, nil
}

func SignWithEIP712Hash(eip712Hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	sig, err := crypto.Sign(eip712Hash, prv)
	if err != nil {
		return nil, err
	}

	// v = v + 27
	sig[64] += 27
	return sig, nil
}
