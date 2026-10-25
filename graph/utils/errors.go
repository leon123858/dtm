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
	if errors.Is(err, domain.ErrUserNotFound) {
		return UserError("UNAUTHENTICATED")
	}
	if errors.Is(err, domain.ErrTripNotFound) || errors.Is(err, domain.ErrSubscriptionNotFound) {
		return UserError("NOT_FOUND")
	}
	return UserError("INTERNAL_SERVER_ERROR")
}
