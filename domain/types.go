package domain

import (
	"time"

	"github.com/google/uuid"
)

type RecordCategory int

const (
	CategoryNormal RecordCategory = iota
	CategoryFix
	CategoryPart
	CategoryFixBeforeNormal
	CategoryTransfer
)

// Address is a trip-scoped participant. ID is the stable identity; Name is
// display data and may be changed without affecting financial relationships.
type Address struct {
	ID   uuid.UUID
	Name string
}

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
	ID             uuid.UUID
	ParentRecordID *uuid.UUID
	ChildRecordID  *uuid.UUID
	Name           string
	Amount         float64
	Time           time.Time
	PrePayAddress  Address
	Category       RecordCategory
	IsDeleted      bool
}

type RecordData struct {
	ShouldPayAddress []ExtendAddress
}

type Record struct {
	RecordInfo
	RecordData
}
