package testutils

import (
	"errors"

	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// to simplify testing, default is one token
type wallet struct {
	nonces  *uint256.Int
	balance *uint256.Int
}

type MockFollower struct {
	addrMap          map[gethcmn.Address]gethcmn.Address
	wallets          map[gethcmn.Address]*wallet
	validatorPubKeys [][]byte // compressed public keys
}

func NewMockFollower(addrMap map[gethcmn.Address]gethcmn.Address, validatorPubKeys [][]byte) *MockFollower {
	return &MockFollower{
		addrMap:          addrMap,
		wallets:          make(map[gethcmn.Address]*wallet),
		validatorPubKeys: validatorPubKeys,
	}
}

func (m *MockFollower) SetWallet(address gethcmn.Address, nonces, balance *uint256.Int) {
	m.wallets[address] = &wallet{
		nonces:  nonces,
		balance: balance,
	}
}

func (m *MockFollower) GetDelegatedAddrByMainAddr(mainAddr gethcmn.Address) (gethcmn.Address, error) {
	delegatedAddr, found := m.addrMap[mainAddr]
	if !found {
		return gethcmn.Address{}, errors.New("cannot get delegated address")
	}
	return delegatedAddr, nil
}

func (m *MockFollower) LoadWalletInStochasticPay(tokenAddress, ownerAddress gethcmn.Address) (*uint256.Int, *uint256.Int, error) {
	w, found := m.wallets[ownerAddress]
	if !found {
		return uint256.NewInt(0), uint256.NewInt(0), nil
	}
	return w.nonces, w.balance, nil
}

func (m *MockFollower) GetValidatorPubKeyList() [][]byte {
	return m.validatorPubKeys
}
