package utils

import (
	"dtm/graph/model"
	"dtm/libs/textvalidate"
	"fmt"
	"time"
)

// NormalizeRecordRequest handles only transport-shape concerns that cannot be
// represented by domain.Record. Domain validation belongs to the trip service.
func NormalizeRecordRequest(r *model.NewRecord) bool {
	if len(r.ExtendPayMsg) > len(r.ShouldPayAddressIds) {
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
	timestamp, err := textvalidate.ParseTimestamp(jsTimestampStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse timestamp string '%s' to int64: %w", jsTimestampStr, err)
	}
	return timestamp, nil
}
