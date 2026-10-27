package domain

import (
	"time"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
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
// the trip nickname and may be changed without affecting financial relationships.
type Address struct {
	UseNickname bool
	ID          uuid.UUID
	Name        string
}

type ExtendAddress struct {
	Address   Address
	ExtendMsg float64
}

type TripInfo struct {
	Private bool
	ID      uuid.UUID
	Name    string
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

type User struct {
	ID      uuid.UUID
	GCID    string
	Name    *string
	Email   *string
	SubMail bool
}

// SubscribedTrip is a user-specific trip subscription.
type SubscribedTrip struct {
	ID           uuid.UUID
	Name         string
	LastViewedAt time.Time
}

type TripBinding struct {
	TripID  uuid.UUID
	Address *Address
}

// RecordFields is the common diff/patch target for client snapshots and the
// transaction-time tail. It deliberately excludes identity, links and names of
// addresses. IDs, milliseconds and category numbers use canonical strings so
// a malformed old client baseline can still be compared when repairing it.
type RecordFields struct {
	Name             string
	Amount           float64
	Time             string
	PrePayAddressID  string
	Category         string
	ShouldPayAddress RecordShares `diff:"ShouldPayAddress,omitunequal"`
	IsDeleted        bool
}

type RecordShare struct {
	AddressID string `diff:"AddressID,identifier"`
	ExtendMsg float64
}

// RecordShares identifies members by AddressID; order is not an edit.
type RecordShares []RecordShare

// RecordPatch contains an in-process r3labs changelog, not a serialized patch.
// Obtain patches from recordpatch.Diff; consumers trust its paths and value types.
// Changes target editable scalar fields or identified share members.
type RecordPatch struct {
	Changes odiff.Changelog
}

type userContextKey struct{}
