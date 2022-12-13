package ugo

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/holiman/uint256"
)

// return uint256, overflow
func BytesToUint256(numBz []byte) (*uint256.Int, bool) {
	hexBz := hexutil.Encode(numBz)
	bigInt, _ := new(big.Int).SetString(hexBz, 0)
	return uint256.FromBig(bigInt)
}

// return bigint, overflow
func BytesToBig(numBz []byte) *big.Int {
	hexBz := hexutil.Encode(numBz)
	bigInt, _ := new(big.Int).SetString(hexBz, 0)
	return bigInt
}
