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

type Record struct {
	ID               uuid.UUID
	Name             string
	Amount           float64
	PrePayAddress    Address
	ShouldPayAddress []Address
}
