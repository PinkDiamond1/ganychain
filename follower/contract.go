package follower

import (
	"crypto/sha256"
	"errors"

	gethcmn "github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	mevmtypes "github.com/smartbch/moeingevm/types"
)

const (
	// TODO: change it if necessary
	// ganyaccount
	GanyAccountContractSeq   = 288
	DelegatedAccountsMapSlot = 0

	// stochasticpay
	StochasticPayVRFSeq = 506

	// ganygov
	GanyGovSeq     = 509
	ValidatorsSlot = 1
	ValidatorWords = 9
)

/*
	mapping(address => address) private delegatedAccountsMap; // main account => delegated account
*/

// Note: `mainAddr` must be valid hex address
func getDelegatedAddrByMainAddr(ctx *mevmtypes.Context, mainAddress gethcmn.Address) (gethcmn.Address, error) {
	addrBz, err := gethcmn.ParseHexOrString(mainAddress.Hex())
	if err != nil {
		return gethcmn.Address{}, err
	}

	keyByte32 := uint256.NewInt(0).SetBytes(addrBz).PaddedBytes(32)
	posByte32 := uint256.NewInt(DelegatedAccountsMapSlot).PaddedBytes(32)
	var bzNeedToHash [64]byte
	copy(bzNeedToHash[:32], keyByte32)
	copy(bzNeedToHash[32:], posByte32)
	accountLoc := uint256.NewInt(0).SetBytes(gethcrypto.Keccak256(bzNeedToHash[:]))
	result := ctx.GetStorageAt(GanyAccountContractSeq, string(accountLoc.PaddedBytes(32)))
	if result == nil {
		return gethcmn.Address{}, errors.New("cannot find account in storage")
	}
	resultAddr := gethcmn.BytesToAddress(result[12:])
	return resultAddr, nil
}

/*
	The wallet of StochasticPayVRF.sol is SEP101 storage
	Key:  abi.encode(sep20Contract, owner)
	Value: abi.decode(valueBz, (uint, uint))
*/

// Note: `tokenAddr` and `ownerAddr` must be valid hex address
func loadWalletInStochasticPay(ctx *mevmtypes.Context, tokenAddress, ownerAddress gethcmn.Address) (*uint256.Int, *uint256.Int, error) {
	tokenAddrBz, err := gethcmn.ParseHexOrString(tokenAddress.Hex())
	if err != nil {
		return nil, nil, err
	}

	ownerAddrBz, err := gethcmn.ParseHexOrString(ownerAddress.Hex())
	if err != nil {
		return nil, nil, err
	}

	tokenAddrBz32 := uint256.NewInt(0).SetBytes(tokenAddrBz).PaddedBytes(32)
	ownerAddrBz32 := uint256.NewInt(0).SetBytes(ownerAddrBz).PaddedBytes(32)
	var bzNeedToHash [64]byte
	copy(bzNeedToHash[:32], tokenAddrBz32)
	copy(bzNeedToHash[32:], ownerAddrBz32)

	sKey := sha256.Sum256(bzNeedToHash[:])
	result := ctx.GetStorageAt(StochasticPayVRFSeq, string(sKey[:]))
	if result == nil {
		return nil, nil, errors.New("cannot load wallet in storage")
	}

	if len(result) != 64 {
		return nil, nil, errors.New("the result length must be 64")
	}

	nonces := uint256.NewInt(0).SetBytes(result[:32])
	balance := uint256.NewInt(0).SetBytes(result[32:])
	return nonces, balance, nil
}

/*
   struct ValidatorInfo {
       address addr;           // address
       uint    pubkeyPrefix;   // 0x02 or 0x03
       bytes32 pubkeyX;        // x
       bytes32 rpcUrl;         // ip:port
       bytes32 intro;          // introduction
       uint    totalStakedAmt; // total staked BCH
       uint    selfStakedAmt;  // self staked BCH
       uint    electedTime;    // 0 means not elected, set by Golang
       uint    oldElectedTime; // used to get old operators, set by Golang
   }
*/

type ValidatorInfo struct {
	Addr           gethcmn.Address
	Pubkey         []byte // 33 bytes
	RpcUrl         []byte // 32 bytes
	Intro          []byte // 32 bytes
	TotalStakedAmt *uint256.Int
	SelfStakedAmt  *uint256.Int
	ElectedTime    *uint256.Int
	OldElectedTime *uint256.Int
}

func getValidatorPubKeyList(ctx *mevmtypes.Context) [][]byte {
	validatorInfos := readValidatorInfos(ctx, GanyGovSeq)
	result := make([][]byte, 0, len(validatorInfos))
	for _, vi := range validatorInfos {
		result = append(result, vi.Pubkey)
	}
	return result
}

func readValidatorInfos(ctx *mevmtypes.Context, seq uint64) []*ValidatorInfo {
	arrSlot := uint256.NewInt(ValidatorsSlot).PaddedBytes(32)
	arrLen := uint256.NewInt(0).SetBytes(ctx.GetStorageAt(seq, string(arrSlot)))
	arrLoc := uint256.NewInt(0).SetBytes(gethcrypto.Keccak256(arrSlot))

	result := make([]*ValidatorInfo, 0, arrLen.Uint64())
	for i := uint64(0); i < arrLen.Uint64(); i++ {
		loc := uint256.NewInt(0).AddUint64(arrLoc, i*ValidatorWords)
		result = append(result, readValidatorInfo(ctx, seq, loc))
	}
	return result
}

func readValidatorInfo(ctx *mevmtypes.Context, seq uint64, loc *uint256.Int) *ValidatorInfo {
	addr := ctx.GetStorageAt(seq, string(loc.PaddedBytes(32)))                             // slot#0
	pubkeyPrefix := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32)))   // slot#1
	pubkeyX := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32)))        // slot#2
	rpcUrl := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32)))         // slot#3
	intro := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32)))          // slot#4
	totalStakedAmt := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32))) // slot#5
	selfStakedAmt := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32)))  // slot#6
	electedTime := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32)))    // slot#7
	oldElectedTime := ctx.GetStorageAt(seq, string(loc.AddUint64(loc, 1).PaddedBytes(32))) // slot#8

	return &ValidatorInfo{
		Addr:           gethcmn.BytesToAddress(addr),
		Pubkey:         append(pubkeyPrefix[31:], pubkeyX...),
		RpcUrl:         rpcUrl[:],
		Intro:          intro[:],
		TotalStakedAmt: uint256.NewInt(0).SetBytes(totalStakedAmt),
		SelfStakedAmt:  uint256.NewInt(0).SetBytes(selfStakedAmt),
		ElectedTime:    uint256.NewInt(0).SetBytes(electedTime),
		OldElectedTime: uint256.NewInt(0).SetBytes(oldElectedTime),
	}
}
