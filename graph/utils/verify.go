package utils

import (
	"dtm/graph/model"
	"fmt"
	"strconv"
	"time"
	"unicode"
)

func IsSecureString(s string) bool {
	// 定義允許的「安全符號」
	allowedSafeSymbols := map[rune]bool{
		'_': true,
		'-': true,
		'.': true,
		'@': true,
		'#': true,
		' ': true,
	}

	for _, r := range s {
		// 如果不是字母，也不是數字
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			// 進一步檢查它是否在允許的安全符號清單中
			if _, ok := allowedSafeSymbols[r]; !ok {
				return false // 發現不允許的特殊字元
			}
		}
	}
	return true // 所有字元都符合安全規範
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

func VerifyRecordRequest(r model.NewRecord) bool {
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