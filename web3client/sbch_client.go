package web3client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	modbtypes "github.com/smartbch/moeingdb/types"
)

const (
	ReqStrSyncBlock = `{"jsonrpc": "2.0", "method": "sbch_getSyncBlock", "params": ["%s"], "id":1}`
	ReqStrBlockNum  = `{"jsonrpc": "2.0", "method": "eth_blockNumber", "params": [], "id":1}`
)

type SbchClient struct {
	*ethclient.Client
	url string
}

func NewSbchClient(url string) *SbchClient {
	if url == "" {
		url = "http://0.0.0.0:8545"
	}

	ethClient, err := ethclient.Dial(url)
	if err != nil {
		panic(err)
	}

	return &SbchClient{
		Client: ethClient,
		url:    url,
	}
}

func (client *SbchClient) sendRequest(reqStr string) ([]byte, error) {
	body := strings.NewReader(reqStr)
	req, err := http.NewRequest("POST", client.url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	return respData, nil
}

type jsonrpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type jsonrpcMessage struct {
	Version string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func (client *SbchClient) GetSyncBlock(height uint64) (*modbtypes.ExtendedBlock, error) {
	respData, err := client.sendRequest(fmt.Sprintf(ReqStrSyncBlock, hexutil.Uint64(height).String()))
	if err != nil {
		return nil, err
	}

	var m jsonrpcMessage
	err = json.Unmarshal(respData, &m)
	if err != nil {
		return nil, err
	}
	var eBlockString string
	err = json.Unmarshal(m.Result, &eBlockString)
	if err != nil {
		return nil, err
	}
	var eBlockBytes []byte
	eBlockBytes, err = hexutil.Decode(eBlockString)
	if err != nil {
		return nil, err
	}
	var eBlock modbtypes.ExtendedBlock
	_, err = eBlock.UnmarshalMsg(eBlockBytes)
	if err != nil {
		return nil, err
	}
	return &eBlock, nil
}

func (client *SbchClient) GeLatestBlockHeight() (int64, error) {
	respData, err := client.sendRequest(ReqStrBlockNum)
	if err != nil {
		return -1, err
	}
	var m jsonrpcMessage
	err = json.Unmarshal(respData, &m)
	if err != nil {
		return -1, err
	}
	var latestBlockHeight hexutil.Uint64
	err = json.Unmarshal(m.Result, &latestBlockHeight)
	if err != nil {
		return -1, err
	}
	return int64(latestBlockHeight), nil
}
