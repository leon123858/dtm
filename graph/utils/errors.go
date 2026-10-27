package utils

import (
	"dtm/domain"
	"errors"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

func UserError(code string) error {
	return &gqlerror.Error{Message: code, Extensions: map[string]any{"code": code}}
}

func MemberError(err error) error {
	if mapped := TripError(err); mapped != err {
		return mapped
	}
	if errors.Is(err, domain.ErrUserNotFound) {
		return UserError("UNAUTHENTICATED")
	}
	if errors.Is(err, domain.ErrTripNotFound) || errors.Is(err, domain.ErrSubscriptionNotFound) {
		return UserError("NOT_FOUND")
	}
	return UserError("INTERNAL_SERVER_ERROR")
}

// TripError preserves existing validation messages and maps domain authorization errors.
func TripError(err error) error {
	if err == nil {
		return nil
	}
	for _, item := range []struct {
		err  error
		code string
	}{
		{domain.ErrUnauthenticated, "UNAUTHENTICATED"}, {domain.ErrUserNotFound, "UNAUTHENTICATED"},
		{domain.ErrForbidden, "FORBIDDEN"}, {domain.ErrConflict, "CONFLICT"},
		{domain.ErrBadUserInput, "BAD_USER_INPUT"}, {domain.ErrTripNotFound, "NOT_FOUND"}, {domain.ErrRecordNotFound, "NOT_FOUND"}, {domain.ErrAddressNotFound, "NOT_FOUND"},
	} {
		if errors.Is(err, item.err) {
			return UserError(item.code)
		}
	}
	return err
}
