package app

import "errors"

var (
	// Bulletin
	ErrKeyNotFound           = errors.New("key not found")
	ErrMainKeyHeadNotFound   = errors.New("main key head not found")
	ErrTimestampTooLong      = errors.New("timestamp is too long ago")
	ErrInvalidOldSN          = errors.New("invalid old SN")
	ErrCantFindOldBulletin   = errors.New("can't find old bulletin")
	ErrCantOverwriteBulletin = errors.New("can't overwrite old bulletin")
)
