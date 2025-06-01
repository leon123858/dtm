package db

import "github.com/google/uuid"

type Address string

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
	PrePayAddress Address
}

type RecordData struct {
	ShouldPayAddress []Address
}

type Record struct {
	RecordInfo
	RecordData
}
