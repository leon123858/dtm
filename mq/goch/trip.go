package goch

import (
	"dtm/mq/mq"
)

// ChannelTripRecordMessageQueue implements TripRecordMessageQueue using a Go channel.
type ChannelTripRecordMessageQueue struct {
	action  mq.Action
	channel chan mq.TripRecordMessage
}

// This struct can be used to implement the TripMessageQueueWrapper interface
type GoChanTripMessageQueueWrapper struct {
	RecordMQArray  [mq.ActionCnt]mq.TripRecordMessageQueue
	AddressMQArray [mq.ActionCnt]mq.TripAddressMessageQueue
}

func (wrapper *GoChanTripMessageQueueWrapper) GetTripRecordMessageQueue(action mq.Action) mq.TripRecordMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle the error as needed
	}
	return wrapper.RecordMQArray[action]
}

func (wrapper *GoChanTripMessageQueueWrapper) GetTripAddressMessageQueue(action mq.Action) mq.TripAddressMessageQueue {
	if action < 0 || action >= mq.ActionCnt {
		return nil // or handle the error as needed
	}
	return wrapper.AddressMQArray[action]
}

// NewGoChanTripMessageQueueWrapper creates a new instance of GoChanTripMessageQueueWrapper.
func NewGoChanTripMessageQueueWrapper() mq.TripMessageQueueWrapper {
	wrapper := GoChanTripMessageQueueWrapper{}
	// address need add and remove
	wrapper.AddressMQArray[mq.ActionCreate] = NewChannelTripAddressMessageQueue(mq.ActionCreate, 0)
	wrapper.AddressMQArray[mq.ActionDelete] = NewChannelTripAddressMessageQueue(mq.ActionDelete, 0)
	// record need add, update and delete
	wrapper.RecordMQArray[mq.ActionCreate] = NewChannelTripRecordMessageQueue(mq.ActionCreate, 0)
	wrapper.RecordMQArray[mq.ActionUpdate] = NewChannelTripRecordMessageQueue(mq.ActionUpdate, 0)
	wrapper.RecordMQArray[mq.ActionDelete] = NewChannelTripRecordMessageQueue(mq.ActionDelete, 0)

	return &wrapper
}

// NewChannelTripRecordMessageQueue creates a new instance of ChannelTripRecordMessageQueue.
// bufferSize determines the capacity of the channel. A bufferSize of 0 means unbuffered.
func NewChannelTripRecordMessageQueue(action mq.Action, bufferSize int) *ChannelTripRecordMessageQueue {
	if bufferSize > 0 {
		return &ChannelTripRecordMessageQueue{
			action:  action,
			channel: make(chan mq.TripRecordMessage, bufferSize),
		}
	}
	// If bufferSize is 0, create an unbuffered channel.
	return &ChannelTripRecordMessageQueue{
		action:  action,
		channel: make(chan mq.TripRecordMessage),
	}
}

// GetAction returns the action associated with this queue.
func (q *ChannelTripRecordMessageQueue) GetAction() mq.Action {
	return q.action
}

// Publish sends a TripRecordMessage to the queue.
func (q *ChannelTripRecordMessageQueue) Publish(msg mq.TripRecordMessage) error {
	// Non-blocking send using a select statement.
	// This prevents the publisher from blocking indefinitely if the channel is full and no receiver is ready.
	select {
	case q.channel <- msg:
		return nil
	default:
		// You might want to log this or return a more specific error
		return ErrQueueFull // Example error for a full channel
	}
}

// Subscribe returns a read-only channel for TripRecordMessages.
func (q *ChannelTripRecordMessageQueue) Subscribe() (<-chan mq.TripRecordMessage, error) {
	return q.channel, nil
}

// ChannelTripAddressMessageQueue implements TripAddressMessageQueue using a Go channel.
type ChannelTripAddressMessageQueue struct {
	action  mq.Action
	channel chan mq.TripAddressMessage
}

// NewChannelTripAddressMessageQueue creates a new instance of ChannelTripAddressMessageQueue.
// bufferSize determines the capacity of the channel. A bufferSize of 0 means unbuffered.
func NewChannelTripAddressMessageQueue(action mq.Action, bufferSize int) *ChannelTripAddressMessageQueue {
	if bufferSize > 0 {
		return &ChannelTripAddressMessageQueue{
			action:  action,
			channel: make(chan mq.TripAddressMessage, bufferSize),
		}
	}
	return &ChannelTripAddressMessageQueue{
		action:  action,
		channel: make(chan mq.TripAddressMessage),
	}
}

// GetAction returns the action associated with this queue.
func (q *ChannelTripAddressMessageQueue) GetAction() mq.Action {
	return q.action
}

// Publish sends a TripAddressMessage to the queue.
func (q *ChannelTripAddressMessageQueue) Publish(msg mq.TripAddressMessage) error {
	select {
	case q.channel <- msg:
		return nil
	default:
		return ErrQueueFull // Example error for a full channel
	}
}

// Subscribe returns a read-only channel for TripAddressMessages.
func (q *ChannelTripAddressMessageQueue) Subscribe() (<-chan mq.TripAddressMessage, error) {
	return q.channel, nil
}

// --- Error Definitions ---
type QueueError string

func (e QueueError) Error() string {
	return string(e)
}

const (
	ErrQueueFull QueueError = "message queue is full"
)
