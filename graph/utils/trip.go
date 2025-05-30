package utils

import (
	"dtm/graph/model"
	"dtm/mq/mq"
	"fmt"

	"github.com/google/uuid"
)

func TripRecordMQ2GQL(msg mq.TripRecordMessage) (*model.Record, bool, error) {
	if msg.ID == uuid.Nil {
		// 返回 nil, true, nil 表示沒有 Record 對象，但應該跳過這個訊息，且沒有錯誤。
		return nil, true, nil
	}

	// 執行您原始範例中的型別轉換邏輯
	record := &model.Record{
		ID:            msg.ID.String(), // 將 uuid.UUID 轉換為 string
		Name:          msg.Name,
		Amount:        msg.Amount,
		PrePayAddress: string(msg.PrePayAddress), // 將 []byte 轉換為 string
	}

	// 成功轉換，返回 record, false (不跳過), nil (無錯誤)
	return record, false, nil
}

func TripRecordIdMQ2GQL(msg mq.TripRecordMessage) (string, bool, error){
	if msg.ID == uuid.Nil {
		// 返回 nil, true, nil 表示沒有 Record 對象，但應該跳過這個訊息，且沒有錯誤。
		return "", true, nil
	}
	
	// 成功轉換，返回 record, false (不跳過), nil (無錯誤)
	return msg.ID.String(), false, nil
}

func TripAddressMQ2GQL(msg mq.TripAddressMessage) (string, bool, error) {
	if len(msg.Address) == 0 {
		fmt.Println("TripAddressMQ2GQL: Empty address in message, skipping.")
		return "", true, nil
	}
	address := string(msg.Address)

	return address, false, nil
}
