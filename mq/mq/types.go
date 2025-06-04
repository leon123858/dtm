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
	TripID        uuid.UUID
	Name          string
	Amount        float64
	PrePayAddress db.Address
}

func (m TripRecordMessage) GetTopic() uuid.UUID {
	return m.TripID
}

type TripAddressMessage struct {
	TripID  uuid.UUID
	Address db.Address
}

func (m TripAddressMessage) GetTopic() uuid.UUID {
	return m.TripID
}
