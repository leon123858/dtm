package mq

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Subscriber 介面定義了任何可被訂閱和取消訂閱的服務所需的方法。
// 這是一個泛型介面，`M` 代表其訂閱的訊息型別。
type Subscriber[M any] interface {
	Subscribe(uuid.UUID) (uuid.UUID, <-chan M, error)
	DeSubscribe(id uuid.UUID) error
}

// SubscribeProcessor 是一個泛型函式，現在接受任何實現 Subscriber 介面的型別 S。
// S 是您想支援的訊息佇列服務類型 (e.g., *TripMQ, *OtherMessageQueue)。
// M 是該服務訂閱的訊息類型。
// O 是輸出結果的型別。
func SubscribeProcessor[S Subscriber[M], M any, O any](
	topicId uuid.UUID, // 訂閱的主題 ID
	ctx context.Context,
	service S, // 傳入實現 Subscriber 介面的服務實例
	transformFunc func(msg M) (O, bool, error),
	outputStream chan<- O,
) {
	go func() {
		// 1. 執行訂閱
		uid, inputCh, err := service.Subscribe(topicId)
		if err != nil {
			return
		}

		// 在 goroutine 結束時，自動取消訂閱
		defer func() {
			if err := service.DeSubscribe(uid); err != nil {
				fmt.Printf("Error de-subscribing %s: %v\n", uid, err)
			} else {
				// fmt.Printf("De-subscribed %s successfully.\n", uid)
			}
			close(outputStream) // 如果每個訂閱者獨佔 outputStream，可以考慮在這裡關閉
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
					// fmt.Printf("Message skipped for ID %s based on transform function.\n", uid) // 根據需要決定是否印出
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
