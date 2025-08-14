package mq

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Subscriber[M any] interface {
	Subscribe(uuid.UUID) (uuid.UUID, <-chan M, error)
	DeSubscribe(id uuid.UUID) error
}

// SubscribeProcessor is a generic function that accepts any type S implementing the Subscriber interface.
// S is the message queue service type you want to support (e.g., *TripMQ, *OtherMessageQueue).
// M is the message type subscribed from the service.
// O is the output result type.
func SubscribeProcessor[S Subscriber[M], M any, O any](
	topicId uuid.UUID,
	ctx context.Context,
	service S,
	transformFunc func(msg M) (O, bool, error),
	outputStream chan<- O,
) {
	go func() {

		uid, inputCh, err := service.Subscribe(topicId)
		if err != nil {
			return
		}

		defer func() {
			if err := service.DeSubscribe(uid); err != nil {
				fmt.Printf("Error de-subscribing %s: %v\n", uid, err)
			} else {
				// fmt.Printf("De-subscribed %s successfully.\n", uid)
			}
			close(outputStream)
		}()

		for {
			select {
			case msg, ok := <-inputCh:
				if !ok {
					// parent close channel
					return
				}

				output, skip, err := transformFunc(msg)
				if err != nil {
					// fmt.Printf("Error transforming message for ID %s: %v. Skipping.\n", uid, err)
					continue
				}
				if skip {
					// fmt.Printf("Message skipped for ID %s based on transform function.\n", uid)
					continue
				}

				select {
				case outputStream <- output:
					// Message sent successfully
				case <-ctx.Done():
					// fmt.Printf("Context cancelled while sending message to outputStream for ID %s. Cleaning up.\n", uid)
					return
				}

			case <-ctx.Done():
				// fmt.Printf("Context for subscription ID %s cancelled. Cleaning up.\n", uid)
				return
			}
		}
	}()
}
