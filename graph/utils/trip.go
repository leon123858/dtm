package utils

import (
	"dtm/adapters/mq/mq"
	"dtm/graph/model"

	"github.com/google/uuid"
)

func TripRecordMQ2GQL(msg mq.TripRecordMessage) (*model.Record, bool, error) {
	if msg.ID == uuid.Nil {
		// do not have record
		return nil, true, nil
	}

	category, err := Int2RecordCategoryChecked(msg.Category)
	if err != nil {
		return nil, false, err
	}
	record := &model.Record{
		ID:         msg.ID.String(),
		Time:       msg.Time,
		Category:   category,
		IsDeleted:  msg.IsDeleted,
		IsActive:   true,
		EventValid: true,
	}
	if msg.ParentRecordID != nil {
		parent := msg.ParentRecordID.String()
		record.ParentRecordID = &parent
	}
	record.Name = &msg.Name
	record.Amount = &msg.Amount
	record.PrePayAddress = ToModelAddress(msg.PrePayAddress)

	return record, false, nil
}

func TripAddressMQ2GQL(msg mq.TripAddressMessage) (*model.Address, bool, error) {
	if msg.Address.ID == uuid.Nil {
		return nil, true, nil
	}
	return ToModelAddress(msg.Address), false, nil
}
