package cryptoutils

import (
	"crypto/ecdsa"
	"errors"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

func PublicKeyToXY(pubKey ecdsa.PublicKey) ([]byte, error) {
	x256, overflow := uint256.FromBig(pubKey.X)
	if overflow {
		return nil, errors.New("public key X overflow")
	}
	y256, overflow := uint256.FromBig(pubKey.Y)
	if overflow {
		return nil, errors.New("public key Y overflow")
	}

	var result [64]byte
	xBz := x256.Bytes32()
	yBz := y256.Bytes32()
	copy(result[:32], xBz[:])
	copy(result[32:], yBz[:])

	return result[:], nil
}

func CompressedPubKeyToXY(cpk []byte) ([]byte, error) {
	if len(cpk) != 33 {
		return nil, errors.New("invalid compressed public key lengths")
	}

	pubkey, err := gethcrypto.DecompressPubkey(cpk)
	if err != nil {
		return nil, err
	}

	return PublicKeyToXY(*pubkey)
}
