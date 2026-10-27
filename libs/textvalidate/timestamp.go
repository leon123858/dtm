package textvalidate

import (
	"strconv"
	"time"
)

// ParseTimestamp parses signed int64 Unix milliseconds, rejecting overflow.
func ParseTimestamp(value string) (time.Time, error) {
	millis, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(millis), nil
}
