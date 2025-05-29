package mq

type TripMessageQueueWrapper interface {
	GetTripRecordMessageQueue(action Action) TripRecordMessageQueue
	GetTripAddressMessageQueue(action Action) TripAddressMessageQueue
}

type TripRecordMessageQueue interface {
	GetAction() Action
	Publish(msg TripRecordMessage) error
	Subscribe() (<-chan TripRecordMessage, error)
}

type TripAddressMessageQueue interface {
	GetAction() Action
	Publish(msg TripAddressMessage) error
	Subscribe() (<-chan TripAddressMessage, error)
}
