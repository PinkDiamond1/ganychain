package api

import (
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/smartbch/ganychain/backend"
)

const (
	namespaceGany = "gany"

	apiVersion = "1.0"
)

// GetAPIs returns the list of all APIs from the Ethereum namespaces
func GetAPIs(backend backend.BackendService, logger tmlog.Logger) []gethrpc.API {

	logger = logger.With("module", "json-rpc")
	_ganyAPI := newGanyAPI(backend, logger)

	return []gethrpc.API{
		{
			Namespace: namespaceGany,
			Version:   apiVersion,
			Service:   _ganyAPI,
			Public:    true,
		},
	}
}
