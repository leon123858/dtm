package utils

import (
	"dtm/domain"
	"dtm/libs/textvalidate"
	"fmt"
)

func ValidateProfile(name *string, subMail *bool, email *string) error {
	if name != nil {
		if err := textvalidate.ValidateName(*name); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrBadUserInput, err)
		}
	}
	if subMail != nil && *subMail && (email == nil || *email == "") {
		return fmt.Errorf("%w: email is required", domain.ErrBadUserInput)
	}
	return nil
}

// AddressName selects an original name for a newly created participant.
func AddressName(name, userName *string, bind bool, exists func(string) bool) (string, error) {
	if name != nil {
		if err := textvalidate.ValidateName(*name); err != nil {
			return "", fmt.Errorf("%w: %v", domain.ErrBadUserInput, err)
		}
		return *name, nil
	}
	if !bind || userName == nil || textvalidate.ValidateName(*userName) != nil {
		return "", fmt.Errorf("%w: address name is required", domain.ErrBadUserInput)
	}
	base := []rune(*userName)
	candidate := string(base)
	for n := 2; exists(candidate); n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		limit := 100 - len([]rune(suffix))
		b := base
		if len(b) > limit {
			b = b[:limit]
		}
		candidate = string(b) + suffix
	}
	return candidate, nil
}
