package cryptoutils

import (
	"bytes"
	"fmt"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/smartbch/merkletree"
)

type MerkleLeaf struct {
	Bz []byte
}

// CalculateHash hashes the values of a Leaf
func (l MerkleLeaf) CalculateHash() ([]byte, error) {
	h := gethcrypto.NewKeccakState()
	if _, err := h.Write(l.Bz); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Equals tests for equality of two Contents
func (l MerkleLeaf) Equals(other merkletree.Content) (bool, error) {
	return bytes.Equal(l.Bz, other.(MerkleLeaf).Bz), nil
}

func VerifyContentExternal(from, rootHash []byte, proof [][]byte) (bool, error) {
	var calHash [32]byte
	fromHash := gethcrypto.Keccak256Hash(from)
	copy(calHash[:], fromHash[:])

	fmt.Printf("calHash: %v\n", calHash)
	fmt.Printf("rootHash: %v\n", rootHash)

	h := gethcrypto.NewKeccakState()
	if len(proof) > 0 {
		for _, p := range proof {
			if _, err := h.Write(combineTwoHash(calHash[:], p)); err != nil {
				return false, err
			}
			copy(calHash[:], h.Sum(nil))
		}
	}

	return bytes.Compare(calHash[:], rootHash) == 0, nil
}

func GetProof(tree *merkletree.MerkleTree, bz []byte) ([][32]byte, error) {
	var result [][32]byte
	c := MerkleLeaf{Bz: bz}
	path, _, err := tree.GetMerklePath(c)
	if len(path) == 0 {
		return result, err
	}

	result = make([][32]byte, len(path))
	for i, p := range path {
		copy(result[i][:], p)
	}

	return result, nil
}

func combineTwoHash(a, b []byte) []byte {
	bf := bytes.NewBuffer(nil)
	if bytes.Compare(a, b) < 0 {
		bf.Write(a)
		bf.Write(b)
		return bf.Bytes()
	}

	bf.Write(b)
	bf.Write(a)
	return bf.Bytes()
}
