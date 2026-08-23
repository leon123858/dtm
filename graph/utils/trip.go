package utils

import (
	"dtm/graph/model"
	"dtm/mq/mq"

	"github.com/google/uuid"
)

func TripRecordMQ2GQL(msg mq.TripRecordMessage) (*model.Record, bool, error) {
	if msg.ID == uuid.Nil {
		// do not have record
		return nil, true, nil
	}

	record := &model.Record{
		ID:            msg.ID.String(),
		Name:          msg.Name,
		Amount:        msg.Amount,
		Time:          msg.Time,
		PrePayAddress: ToModelAddress(msg.PrePayAddress),
		Category:      Int2RecordCategory(msg.Category),
	}

	return record, false, nil
}

func TripRecordIdMQ2GQL(msg mq.TripRecordMessage) (string, bool, error) {
	if msg.ID == uuid.Nil {
		return "", true, nil
	}

	return msg.ID.String(), false, nil
}

func TripAddressMQ2GQL(msg mq.TripAddressMessage) (*model.Address, bool, error) {
	if msg.Address.ID == uuid.Nil {
		return nil, true, nil
	}
	return ToModelAddress(msg.Address), false, nil
}
