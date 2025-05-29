package mq

import (
	"dtm/db/db"

	"github.com/google/uuid"
)

type Action int

const (
	ActionCreate Action = iota
	ActionUpdate
	ActionDelete
	ActionCnt
)

type TripRecordMessage struct {
	ID            uuid.UUID
	Name          string
	Amount        float64
	PrePayAddress db.Address
}

type TripAddressMessage struct {
	Address db.Address
}
