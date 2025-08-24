package utils

import (
	"dtm/graph/model"
	"fmt"
	"strconv"
	"time"
	"unicode"
)

func IsSecureString(s string) bool {
	allowedSafeSymbols := map[rune]bool{
		'_': true,
		'-': true,
		'.': true,
		'@': true,
		'#': true,
		' ': true,
	}

	for _, r := range s {

		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {

			if _, ok := allowedSafeSymbols[r]; !ok {
				return false
			}
		}
	}
	return true
}

func VerifyStringRequest(s string) bool {
	if len(s) == 0 {
		return false
	}
	if len(s) > 100 {
		return false
	}
	for _, char := range s {
		if !IsSecureString(string(char)) {
			return false
		}
	}
	return true
}

func VerifyStringListRequest(s []string) bool {
	if len(s) > 100 {
		return false
	}
	for _, str := range s {
		if !VerifyStringRequest(str) {
			return false
		}
	}
	return true
}

func VerifyFloatListRequest(floats []float64) bool {
	return len(floats) <= 100
}

func VerifyRecordRequestAndSetDefault(r *model.NewRecord) bool {
	if !VerifyStringRequest(r.Name) {
		return false
	}
	if r.Amount <= 0 {
		return false
	}
	if !VerifyStringRequest(string(r.PrePayAddress)) {
		return false
	}
	if !VerifyStringListRequest(r.ShouldPayAddress) {
		return false
	}
	if !VerifyFloatListRequest(r.ExtendPayMsg) {
		return false
	}
	if r.Category != nil && !r.Category.IsValid() {
		return false
	}

	/**
	 * DEFAULT VALUE
	**/
	if r.Category == nil || r.Category.String() == "" {
		modelCategory := model.RecordCategoryNormal
		r.Category = &modelCategory
	}

	return true
}

// ParseJSTimestampString parses a JavaScript Date.now() string (milliseconds since epoch)
// into a Go time.Time object.
func ParseJSTimestampString(jsTimestampStr string) (time.Time, error) {
	unixMilli, err := strconv.ParseInt(jsTimestampStr, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse timestamp string '%s' to int64: %w", jsTimestampStr, err)
	}

	return time.UnixMilli(unixMilli), nil
}
