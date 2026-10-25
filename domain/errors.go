package domain

import "errors"

var (
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrUserNotFound         = errors.New("user not found")
	ErrTripNotFound         = errors.New("trip not found")
	ErrRecordNotFound       = errors.New("record not found")
	ErrInvalidChain         = errors.New("invalid record chain")
)
