package proto

import "errors"

var (
	// GanyTx
	ErrInvalidTxBytes                 = errors.New("invalid gany tx bytes")
	ErrInvalidStochasticPaymentFields = errors.New("invalid stochastic payment fields")
	ErrInvalidBulletinFields          = errors.New("invalid bulletin fields")
)
