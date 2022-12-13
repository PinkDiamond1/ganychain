package proto

import (
	"bytes"
	"encoding/binary"
	"math/big"

	gethcmn "github.com/ethereum/go-ethereum/common"
	gethmath "github.com/ethereum/go-ethereum/common/math"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/smartbch/ganychain/utils/cryptoutils"
	"github.com/smartbch/ganychain/utils/ugo"
)

func (x *AuthProof) GenerateAuthChallenge() *AuthChallenge {
	dynamicSetChallenge := make([]*DynamicSetChallenge, 0, len(x.DynamicSetProofList))
	staticSetChallenge := make([]*StaticSetChallenge, 0, len(x.StaticSetProofList))

	ac := &AuthChallenge{
		StochasticPayCondList: x.StochasticPayCondList,
		OrOfAndOfConditions:   x.OrOfAndOfConditions,
	}

	for _, dsp := range x.DynamicSetProofList {
		dynamicSetChallenge = append(dynamicSetChallenge, &DynamicSetChallenge{
			ChainId:           dsp.ChainId,
			TargetContract:    dsp.TargetContract,
			FunctionSelector:  dsp.FunctionSelector,
			OutData:           dsp.OutData,
			Authenticator:     dsp.Authenticator,
			MaxTimeDifference: dsp.MaxTimeDifference,
		})
	}
	ac.DynamicSetChallengeList = dynamicSetChallenge

	for _, ssp := range x.StaticSetProofList {
		staticSetChallenge = append(staticSetChallenge, &StaticSetChallenge{
			Root: ssp.Root,
		})
	}
	ac.StaticSetChallengeList = staticSetChallenge

	return ac
}

// ----------------------------------------------------------------

type AuthProofType int8

const (
	DynamicProofType           = AuthProofType(1)
	StaticProofType            = AuthProofType(2)
	StochasticPaymentProofType = AuthProofType(3)
)

func (x *AuthProof) CheckAuthProof(sp *StochasticPayment, b *Bulletin) bool {
	if len(x.OrOfAndOfConditions) != 3 {
		return false
	}

	conMap := make(map[int32]AuthProofType) // condition index => AuthProofType

	index := 1
	for range x.DynamicSetProofList {
		conMap[int32(index)] = DynamicProofType
		index++
	}

	for range x.StaticSetProofList {
		conMap[int32(index)] = StaticProofType
		index++
	}

	for range x.StaticSetProofList {
		conMap[int32(index)] = StochasticPaymentProofType
		index++
	}

	orOk := false
	for _, andOfCon := range x.OrOfAndOfConditions {
		if len(andOfCon.ConditionNumbers) == 0 {
			continue
		}

		andOk := true
		for _, conNum := range andOfCon.ConditionNumbers {
			andOk = andOk && x.checkProof(conMap[conNum], conNum, sp, b)
		}
		orOk = orOk || andOk
	}

	return orOk
}

func (x *AuthProof) checkProof(proofType AuthProofType, conditionIndex int32, sp *StochasticPayment, b *Bulletin) bool {
	switch proofType {
	case DynamicProofType:
		return x.checkDynamicProof(conditionIndex, b.From[:])
	case StaticProofType:
		return x.checkStaticProof(conditionIndex, b.From[:])
	case StochasticPaymentProofType:
		return x.checkStochasticPaymentCondition(conditionIndex, sp)
	}

	return false
}

func (x *AuthProof) checkDynamicProof(index int32, from []byte) bool {
	if len(x.DynamicSetProofList) == 0 && len(x.OrOfAndOfConditions[0].ConditionNumbers) == 0 {
		return true
	}

	i := index - 1
	if i < 0 {
		return false
	}

	ei := &ethCallInfo{
		ChainId:   ugo.BytesToBig(x.DynamicSetProofList[i].ChainId),
		Timestamp: new(big.Int).SetInt64(x.DynamicSetProofList[i].Timestamp),
		From:      gethcmn.BytesToAddress(from),
		To:        gethcmn.BytesToAddress(x.DynamicSetProofList[i].TargetContract),
		OutData:   x.DynamicSetProofList[i].OutData,
	}

	binary.BigEndian.PutUint32(ei.FunctionSelector[:], x.DynamicSetProofList[i].FunctionSelector)
	eiBz := ei.ToBytes()
	eiHash := gethcrypto.Keccak256(eiBz)

	pubKey, err := gethcrypto.SigToPub(eiHash[:], x.DynamicSetProofList[i].AuthenticatorSignature)
	if err != nil {
		return false
	}

	if gethcrypto.PubkeyToAddress(*pubKey).Hex() != gethcmn.BytesToAddress(x.DynamicSetProofList[i].Authenticator).Hex() {
		return false
	}
	return false
}

func (x *AuthProof) checkStaticProof(index int32, from []byte) bool {
	if len(x.StaticSetProofList) == 0 && len(x.OrOfAndOfConditions[1].ConditionNumbers) == 0 {
		return true
	}

	i := int(index) - len(x.DynamicSetProofList) - 1
	if i < 0 {
		return false
	}

	isValid, err := cryptoutils.VerifyContentExternal(from, x.StaticSetProofList[i].Root, x.StaticSetProofList[i].Proof)
	if err != nil || !isValid {
		return false
	}

	return true
}

func (x *AuthProof) checkStochasticPaymentCondition(index int32, sp *StochasticPayment) bool {
	if len(x.StochasticPayCondList) == 0 && len(x.OrOfAndOfConditions[2].ConditionNumbers) == 0 {
		return true
	}

	i := int(index) - len(x.DynamicSetProofList) - len(x.StaticSetProofList) - 1
	if i < 0 {
		return false
	}

	return bytes.Equal(x.StochasticPayCondList[i].Payee, sp.Payee) &&
		bytes.Equal(x.StochasticPayCondList[i].Amount, sp.AmountToPayee) &&
		x.StochasticPayCondList[i].Probability == sp.Probability
}

// ----------------------------------------------------------------

type ethCallInfo struct {
	ChainId          *big.Int
	Timestamp        *big.Int
	From             gethcmn.Address
	To               gethcmn.Address
	FunctionSelector [4]byte
	OutData          []byte
}

func (ei *ethCallInfo) ToBytes() []byte {
	bz := make([]byte, 32*4, 32*4+4+len(ei.OutData))
	copy(bz[32*0:32*0+32], gethmath.PaddedBigBytes(ei.ChainId, 32))
	copy(bz[32*1:32*1+32], gethmath.PaddedBigBytes(ei.Timestamp, 32))
	copy(bz[32*2+12:32*2+32], ei.From[:])
	copy(bz[32*3+12:32*3+32], ei.To[:])
	bz = append(bz, ei.FunctionSelector[:]...)
	bz = append(bz, ei.OutData...)
	return bz
}
