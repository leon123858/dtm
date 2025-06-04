package mq

import "github.com/google/uuid"

// TopicProvider 定義了一個可以提供 Topic ID 的介面
type TopicProvider interface {
	GetTopic() uuid.UUID
}

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
