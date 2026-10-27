package domain

import "errors"

var (
	ErrUnauthenticated      = errors.New("authentication required")
	ErrForbidden            = errors.New("trip access forbidden")
	ErrConflict             = errors.New("trip binding conflict")
	ErrBadUserInput         = errors.New("invalid input")
	ErrSubscriptionNotFound = errors.New("subscription not found")
	ErrUserNotFound         = errors.New("user not found")
	ErrTripNotFound         = errors.New("trip not found")
	ErrAddressNotFound      = errors.New("address not found")
	ErrRecordNotFound       = errors.New("record not found")
	ErrInvalidChain         = errors.New("invalid record chain")
)
