package follower

import (
	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

type FollowerService interface {
	GetDelegatedAddrByMainAddr(mainAddr gethcmn.Address) (gethcmn.Address, error)
	LoadWalletInStochasticPay(tokenAddress, ownerAddress gethcmn.Address) (*uint256.Int, *uint256.Int, error)
	GetValidatorPubKeyList() [][]byte
}
