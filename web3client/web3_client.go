package web3client

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	modbtypes "github.com/smartbch/moeingdb/types"
)

type Web3Client interface {
	bind.DeployBackend
	bind.ContractBackend
	GeLatestBlockHeight() (int64, error)
	GetSyncBlock(height uint64) (*modbtypes.ExtendedBlock, error)
}
