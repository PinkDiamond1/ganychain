package proto

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"

	"github.com/golang/protobuf/proto"
)

const (
	TxFieldLengthsLen = 16 // 4 + 4 + 4 + 4
)

// ----------------------------------------------------------------

// GanyTxBz: (TxFieldLengths raw-bytes)16||StochasticPayment||Bulletin||AuthProof||AuthChallenge

type GanyTx []byte

func (tx GanyTx) Len() int { return len(tx) }

// TODO: add a validate function for gant tx
func (tx GanyTx) IsValid() (bool, error) {
	if tx.Len() <= TxFieldLengthsLen {
		return false, ErrInvalidTxBytes
	}

	sp, err := tx.GetStochasticPayment()
	if err != nil {
		return false, ErrInvalidTxBytes
	}

	if !sp.IsValid() {
		return false, ErrInvalidStochasticPaymentFields
	}

	b, err := tx.GetBulletin()
	if err != nil {
		return false, ErrInvalidTxBytes
	}

	if !b.IsValid() {
		return false, ErrInvalidBulletinFields
	}
	return true, nil
}

func (tx GanyTx) GetBytesSlice(start, end int) ([]byte, error) {
	if tx.Len() < end {
		return nil, ErrInvalidTxBytes
	}
	return tx[start:end], nil
}

func (tx GanyTx) GetStochasticPaymentBytes() ([]byte, error) {
	if tx.Len() < TxFieldLengthsLen {
		return nil, ErrInvalidTxBytes
	}

	bzStart := TxFieldLengthsLen
	bzLen := int(binary.BigEndian.Uint32(tx[:4]))
	if bzLen == 0 {
		return nil, nil
	}
	return tx.GetBytesSlice(bzStart, bzStart+bzLen)
}

func (tx GanyTx) GetStochasticPayment() (*StochasticPayment, error) {
	bz, err := tx.GetStochasticPaymentBytes()
	if err != nil {
		return nil, err
	}

	var stochasticPayment StochasticPayment
	err = proto.Unmarshal(bz, &stochasticPayment)
	if err != nil {
		return nil, err
	}

	return &stochasticPayment, nil
}

func (tx GanyTx) GetBulletinBytes() ([]byte, error) {
	if tx.Len() < TxFieldLengthsLen {
		return nil, ErrInvalidTxBytes
	}

	bzStart := int(binary.BigEndian.Uint32(tx[:4])) + TxFieldLengthsLen
	bzLen := int(binary.BigEndian.Uint32(tx[4:8]))
	if bzLen == 0 {
		return nil, nil
	}
	return tx.GetBytesSlice(bzStart, bzStart+bzLen)
}

func (tx GanyTx) GetBulletin() (*Bulletin, error) {
	bz, err := tx.GetBulletinBytes()
	if err != nil {
		return nil, err
	}

	var bulletin Bulletin
	err = proto.Unmarshal(bz, &bulletin)
	if err != nil {
		return nil, err
	}

	return &bulletin, nil
}

func (tx GanyTx) GetAuthProofBytes() ([]byte, error) {
	if tx.Len() < TxFieldLengthsLen {
		return nil, ErrInvalidTxBytes
	}

	bzStart := int(binary.BigEndian.Uint32(tx[:4])+binary.BigEndian.Uint32(tx[4:8])) + TxFieldLengthsLen
	bzLen := int(binary.BigEndian.Uint32(tx[8:12]))
	if bzLen == 0 {
		return nil, nil
	}
	return tx.GetBytesSlice(bzStart, bzStart+bzLen)
}

func (tx GanyTx) GetAuthProof() (*AuthProof, error) {
	bz, err := tx.GetAuthProofBytes()
	if err != nil {
		return nil, err
	}

	var authProof AuthProof
	err = proto.Unmarshal(bz, &authProof)
	if err != nil {
		return nil, err
	}

	return &authProof, nil
}

func (tx GanyTx) GetAuthChanllengeBytes() ([]byte, error) {
	if tx.Len() < TxFieldLengthsLen {
		return nil, ErrInvalidTxBytes
	}

	bzStart := int(binary.BigEndian.Uint32(tx[:4])+binary.BigEndian.Uint32(tx[4:8])+binary.BigEndian.Uint32(tx[8:12])) + TxFieldLengthsLen
	bzLen := int(binary.BigEndian.Uint32(tx[12:16]))
	if bzLen == 0 {
		return nil, nil
	}
	return tx.GetBytesSlice(bzStart, bzStart+bzLen)
}

func (tx GanyTx) GetAuthChanllenge() (*AuthChallenge, error) {
	bz, err := tx.GetAuthChanllengeBytes()
	if err != nil {
		return nil, err
	}

	var authChallenge AuthChallenge
	err = proto.Unmarshal(bz, &authChallenge)
	if err != nil {
		return nil, err
	}

	return &authChallenge, nil
}

// The bulletin's ID has 64 bytes.
// Its first 32 bytes is the sha256 hash of the concatenation of the first two byte strings (`Bulletin` and `AuthProof`).
// Its last 32 bytes is the sha256 hash of the last byte string (`AuthChallenge`).
func (tx GanyTx) GetBulletinID() ([64]byte, error) {
	var id [64]byte

	h := sha256.New()

	bulletinBz, err := tx.GetBulletinBytes()
	if err != nil {
		return id, err
	}

	if len(bulletinBz) > 2 {
		hashWriteBytes(h, bulletinBz[:2])
	}

	authProofBz, err := tx.GetAuthProofBytes()
	if err != nil {
		return id, err
	}

	if len(authProofBz) > 2 {
		hashWriteBytes(h, authProofBz[:2])
	}

	firstHash := h.Sum(nil)
	copy(id[:32], firstHash[:])
	h.Reset()

	authChallengeBz, err := tx.GetAuthChanllengeBytes()
	if err != nil {
		return id, err
	}

	if len(authChallengeBz) > 0 {
		hashWriteBytes(h, authChallengeBz[:])
	}

	lastHash := h.Sum(nil)
	copy(id[32:], lastHash[:])

	return id, nil
}

func CreateGanyTx(sp *StochasticPayment, b *Bulletin, ap *AuthProof, ac *AuthChallenge) GanyTx {
	var ganyTx GanyTx

	var txFieldLengths [TxFieldLengthsLen]byte
	stochasticPaymentBz, _ := proto.Marshal(sp)
	bulletinBz, _ := proto.Marshal(b)
	authProofBz, _ := proto.Marshal(ap)
	authChallengeBz, _ := proto.Marshal(ac)

	binary.BigEndian.PutUint32(txFieldLengths[:4], uint32(len(stochasticPaymentBz)))
	binary.BigEndian.PutUint32(txFieldLengths[4:8], uint32(len(bulletinBz)))
	binary.BigEndian.PutUint32(txFieldLengths[8:12], uint32(len(authProofBz)))
	binary.BigEndian.PutUint32(txFieldLengths[12:], uint32(len(authChallengeBz)))

	ganyTx = append(ganyTx, txFieldLengths[:]...)
	ganyTx = append(ganyTx, stochasticPaymentBz...)
	ganyTx = append(ganyTx, bulletinBz...)
	ganyTx = append(ganyTx, authProofBz...)
	ganyTx = append(ganyTx, authChallengeBz...)

	return ganyTx
}

// ----------------------------------------------------------------

func hashWriteBytes(h hash.Hash, bz []byte) {
	h.Write(bz)
}
