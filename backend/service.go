package backend

import (
	gethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"

	pb "github.com/smartbch/ganychain/proto"
)

type BackendService interface {
	GetAllChainIds() []string
	GetBulletinByGanyUrl(ganyUrlBz []byte) (*pb.Bulletin, error)
	GetGanyTxByGanyUrl(ganyUrlBz []byte) (pb.GanyTx, error)
	QueryBulletinByTimePeriod(typ pb.Bulletin_BulletinType, topicHash [32]byte, start, end int64,
		excludeSNs map[string]struct{}) ([]*pb.Bulletin, error)
	PutBulletin(tx pb.GanyTx) (tmbytes.HexBytes, error)

	GetDelegatedAddr(mainAddress gethcmn.Address) (gethcmn.Address, error)
	LoadWalletInStochasticPay(tokenAddr, ownerAddr gethcmn.Address) (*uint256.Int, *uint256.Int, error)
	GetValidatorPubKeyList() [][]byte
}
