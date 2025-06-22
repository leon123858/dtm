package utils

import "dtm/graph/model"

func VerifyStringRequest(s string) bool {
	if len(s) == 0 {
		return false
	}
	if len(s) > 100 {
		return false
	}
	for _, char := range s {
		if !(('a' <= char && char <= 'z') || ('A' <= char && char <= 'Z') || ('0' <= char && char <= '9') || char == '_' || char == '-') {
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
