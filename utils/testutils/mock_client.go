package testutils

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	modbtypes "github.com/smartbch/moeingdb/types"
)

type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

func (client *MockClient) TransactionReceipt(ctx context.Context, txHash gethcmn.Hash) (*types.Receipt, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) CodeAt(ctx context.Context, account gethcmn.Address, blockNumber *big.Int) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) PendingCodeAt(ctx context.Context, account gethcmn.Address) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) PendingNonceAt(ctx context.Context, account gethcmn.Address) (uint64, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) EstimateGas(ctx context.Context, call ethereum.CallMsg) (gas uint64, err error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	//TODO implement me
	panic("implement me")
}

func (client *MockClient) GetSyncBlock(height uint64) (*modbtypes.ExtendedBlock, error) {
	//TODO implement me
	panic("not implemented")
}

func (client *MockClient) GeLatestBlockHeight() (int64, error) {
	//TODO implement me
	panic("not implemented")
}
