package proto

import (
	"bytes"
	"encoding/binary"
	"time"

	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	eip712types "github.com/ethereum/go-ethereum/signer/core/apitypes"
)

var (
	PayerSaltTemplate = "payerSalt"
)

func (x *StochasticPayment) IsValid() bool {
	return len(x.ValidatorPubkeyHashRoot) == 32 &&
		x.DueTime > 0 &&
		x.Probability > 0 &&
		(x.Payee == nil || len(x.Payee) == gethcmn.AddressLength) &&
		len(x.AmountToPayee) <= 12 &&
		len(x.AmountToValidator) <= 12 &&
		len(x.Signature) == 65

}
func (x *StochasticPayment) GetRSV() ([32]byte, [32]byte, byte) {
	var r [32]byte
	var s [32]byte
	copy(r[:], x.Signature[:32])
	copy(s[:], x.Signature[32:64])
	return r, s, x.Signature[64]
}

func (x *StochasticPayment) GenEIP712MsgForSR(tokenAddr gethcmn.Address) eip712types.TypedDataMessage {
	payerSalt := crypto.Keccak256([]byte(PayerSaltTemplate))
	payerSaltPk0, pkTail := concatPayerSaltPublicKey(payerSalt, x.ValidatorPubkeyHashRoot)

	return eip712types.TypedDataMessage{
		"payerSalt_pk0": hexutil.Encode(payerSaltPk0),
		"pkTail":        hexutil.Encode(pkTail),
		"payeeAddr_dueTime64_prob32": hexutil.Encode(
			concatAddrDueTime64Prob32(gethcmn.BytesToAddress(x.Payee), time.Unix(x.GetDueTime(), 0).Unix(), x.Probability)),
		"seenNonces":           hexutil.Encode(x.Nonces),
		"sep20Contract_amount": hexutil.Encode(concatAddressAmount(tokenAddr, x.AmountToPayee)),
	}
}

func (x *StochasticPayment) GenEIP712MsgForAB(tokenAddr gethcmn.Address) eip712types.TypedDataMessage {
	payerSalt := crypto.Keccak256([]byte(PayerSaltTemplate))

	return eip712types.TypedDataMessage{
		"payerSalt":  hexutil.Encode(payerSalt),
		"pkHashRoot": hexutil.Encode(x.ValidatorPubkeyHashRoot),
		"sep20Contract_dueTime64_prob32": hexutil.Encode(
			concatAddrDueTime64Prob32(tokenAddr, time.Unix(x.GetDueTime(), 0).Unix(), x.Probability)),
		"seenNonces":         hexutil.Encode(x.Nonces),
		"payeeAddrA_amountA": hexutil.Encode(concatAddressAmount(gethcmn.BytesToAddress(x.Payee), x.AmountToPayee)),
		"amountB":            hexutil.Encode(x.AmountToValidator),
	}
}

// ----------------------------------------------------------------

// return payerSalt_pk0, pkTail
func concatPayerSaltPublicKey(payerSalt, pkHash []byte) ([]byte, []byte) {
	var result [31]byte
	copy(result[:30], payerSalt[:30])
	result[30] = pkHash[0]

	// set the highest bit to enable payment
	result[0] = 0x80 | result[0]
	return result[:], pkHash[1:]
}

func concatAddrDueTime64Prob32(payee [20]byte, dueTime int64, prob32 uint32) []byte {
	var result [32]byte
	copy(result[:20], payee[:])
	binary.BigEndian.PutUint64(result[20:28], uint64(dueTime))
	binary.BigEndian.PutUint32(result[28:], prob32)
	return result[:]
}

// NOTE: the length of amount cannot be greater than 12
func concatAddressAmount(token [20]byte, amount []byte) []byte {
	var result [32]byte
	copy(result[:20], token[:])

	// uint32
	buf := bytes.NewBuffer(nil)
	needToFillPrefix := 12 - len(amount)
	for i := 0; i < needToFillPrefix; i++ {
		buf.WriteByte(0x00)
	}
	buf.Write(amount)

	copy(result[20:], buf.Bytes())
	return result[:]
}
