package mq

import (
	"dtm/db/db"

	"github.com/google/uuid"
)

type MqMode string

const (
	MqModeGoChan    MqMode = "gochan"
	MqModeRabbitMQ  MqMode = "rabbitmq"
	MqModeGCPPubSub MqMode = "gcppubsub"
)

type Action int

const (
	ActionCreate Action = iota
	ActionUpdate
	ActionDelete
	ActionCnt
)

// String makes Action compliant with the Stringer interface for cleaner topic names.
func (a Action) String() string {
	switch a {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case ActionDelete:
		return "delete"
	default:
		return "unknown"
	}
}

type TripRecordMessage struct {
	ID            uuid.UUID
	TripID        uuid.UUID
	Name          string
	Amount        float64
	Time          string // ISO format
	PrePayAddress db.Address
	Category      int // 使用整數表示 RecordCategory
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
