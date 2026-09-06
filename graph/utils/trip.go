package utils

import (
	"dtm/adapters/mq/mq"
	"dtm/domain"
	"dtm/graph/model"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

func TripRecordMQ2GQL(msg mq.TripRecordMessage) (*model.Record, bool, error) {
	if msg.ID == uuid.Nil {
		// do not have record
		return nil, true, nil
	}

	var timestamp time.Time
	if msg.Time != "" {
		millis, err := strconv.ParseInt(msg.Time, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("invalid record time: %w", err)
		}
		timestamp = time.UnixMilli(millis)
	}
	record, err := ToModelRecordChecked(domain.Record{
		RecordInfo: domain.RecordInfo{
			ID: msg.ID, Name: msg.Name, Time: timestamp, Amount: msg.Amount,
			PrePayAddress: msg.PrePayAddress, Category: domain.RecordCategory(msg.Category),
			ParentRecordID: msg.ParentRecordID, IsDeleted: msg.IsDeleted,
		},
		RecordData: domain.RecordData{ShouldPayAddress: msg.ShouldPayAddress},
	}, true)
	if err != nil {
		return nil, false, err
	}
	record.Time = msg.Time
	return record, false, nil
}

func TripAddressMQ2GQL(msg mq.TripAddressMessage) (*model.Address, bool, error) {
	if msg.Address.ID == uuid.Nil {
		return nil, true, nil
	}
	return ToModelAddress(msg.Address), false, nil
}
