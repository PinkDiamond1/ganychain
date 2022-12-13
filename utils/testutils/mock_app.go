package testutils

import (
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v3"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmcoretypes "github.com/tendermint/tendermint/rpc/coretypes"

	"github.com/smartbch/ganychain/app"
	pb "github.com/smartbch/ganychain/proto"
)

type MockGanyApp struct {
	*app.GanyApplication
	height int64
}

func NewMockGanyApp(app *app.GanyApplication) *MockGanyApp {
	return &MockGanyApp{
		GanyApplication: app,
		height:          0,
	}
}

func CreateMockGanyApp(db *badger.DB) *MockGanyApp {
	gApp := app.NewGanyApplication(db, "10000", tmlog.MustNewDefaultLogger(tmlog.LogFormatPlain, tmlog.LogLevelInfo, false))
	return NewMockGanyApp(gApp)
}

func (app *MockGanyApp) BroadcastTx(txBz []byte) (*tmcoretypes.ResultBroadcastTxCommit, error) {
	nextHeight := app.height + 1

	tx := pb.GanyTx(txBz)
	b, err := tx.GetBulletin()
	if err != nil {
		return nil, err
	}

	app.BeginBlock(abcitypes.RequestBeginBlock{
		Header: tmproto.Header{
			Height: nextHeight,
			Time:   time.Unix(b.GetTimestamp(), 0),
		},
	})

	resp := app.DeliverTx(abcitypes.RequestDeliverTx{
		Tx: txBz,
	})

	if resp.GetCode() != 0 {
		return nil, fmt.Errorf("code: %v, error: %v", resp.GetCode(), resp.GetLog())
	}

	app.EndBlock(abcitypes.RequestEndBlock{Height: nextHeight})
	app.Commit()

	app.height = nextHeight

	commitResult := &tmcoretypes.ResultBroadcastTxCommit{
		DeliverTx: resp,
		Height:    nextHeight,
	}

	return commitResult, nil
}
