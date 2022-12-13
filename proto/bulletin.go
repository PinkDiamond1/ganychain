package proto

import (
	"bytes"
	"crypto/sha256"

	gethcmn "github.com/ethereum/go-ethereum/common"
)

// ----------------------------------------------------------------

func (x *Bulletin) IsValid() bool {
	return len(x.Topic) > 0 &&
		x.Type >= Bulletin_COMMENT &&
		x.Type <= Bulletin_CENSOR &&
		x.Timestamp > 0 &&
		len(x.From) == gethcmn.AddressLength
}

func (x *Bulletin) CanBeOverwrittenBy(other *Bulletin) bool {
	return bytes.Equal(x.Topic[:], other.Topic[:]) &&
		x.Type == other.Type &&
		x.Timestamp == other.Timestamp &&
		bytes.Equal(x.From[:], other.From[:]) &&
		x.Duration == other.Duration
}

func (x *Bulletin) IsModify() bool {
	return len(x.OldSn) != 0
}

func (x *Bulletin) GetTopicHash() [32]byte {
	return sha256.Sum256(x.Topic[:])
}

// ----------------------------------------------------------------
