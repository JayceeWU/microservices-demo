package domain

import "errors"

var (
	ErrInvalidMoney      = errors.New("invalid money")
	ErrInvalidOrder      = errors.New("invalid order")
	ErrInvalidTransition = errors.New("invalid order transition")
	ErrRefundDenied      = errors.New("refund denied")
)
