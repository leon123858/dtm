package db

import (
	"time"

	"github.com/google/uuid"
)

type RecordCategory int

const (
	CategoryNormal RecordCategory = iota
	CategoryFix
)

type Address string

type ExtendAddress struct {
	Address   Address
	ExtendMsg float64
}

type TripInfo struct {
	ID   uuid.UUID
	Name string
}

type TripData struct {
	Records     []Record
	AddressList []Address
}

type Trip struct {
	TripInfo
	TripData
}

type RecordInfo struct {
	ID            uuid.UUID
	Name          string
	Amount        float64
	Time          time.Time
	PrePayAddress Address
	Category      RecordCategory
}

type RecordData struct {
	ShouldPayAddress []ExtendAddress
}

type Record struct {
	RecordInfo
	RecordData
}
