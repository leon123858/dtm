package mq

import "github.com/google/uuid"

type TripMessageQueueWrapper interface {
	GetTripRecordMessageQueue(action Action) TripRecordMessageQueue
	GetTripAddressMessageQueue(action Action) TripAddressMessageQueue
}

type TripMessageQueue interface {
	GetAction() Action
	Publish(msg interface{}) error
	Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan interface{}, error)
	DeSubscribe(id uuid.UUID) error
}

type TripRecordMessageQueue interface {
	GetAction() Action
	Publish(msg TripRecordMessage) error
	Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan TripRecordMessage, error)
	DeSubscribe(id uuid.UUID) error
}

type TripAddressMessageQueue interface {
	GetAction() Action
	Publish(msg TripAddressMessage) error
	Subscribe(tripId uuid.UUID) (uuid.UUID, <-chan TripAddressMessage, error)
	DeSubscribe(id uuid.UUID) error
}
