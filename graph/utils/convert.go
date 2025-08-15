package utils

import (
	"dtm/db/db"
	"dtm/graph/model"

	"dtm/tx"
	"fmt"
	"time"
)

func RecordCategory2Int(category *model.RecordCategory) int {
	switch *category {
	case model.RecordCategoryNormal:
		return 0
	case model.RecordCategoryFix:
		return 1
	case model.RecordCategoryPart:
		return 2
	default:
		panic("unknown RecordCategory 2 int: " + string(category.String()))
	}
}

func Int2RecordCategory(category int) model.RecordCategory {
	switch category {
	case 0:
		return model.RecordCategoryNormal
	case 1:
		return model.RecordCategoryFix
	case 2:
		return model.RecordCategoryPart
	default:
		panic("unknown RecordCategory: " + string(rune(category)))
	}
}

func ToModelTxList(txList []tx.Tx) []*model.Tx {
	modelList := make([]*model.Tx, len(txList))
	for i, t := range txList {
		modelList[i] = &model.Tx{
			Input: make([]*model.Payment, len(t.Input)),
			Output: &model.Payment{
				Address: t.Output.Address,
				Amount:  t.Output.Amount,
			},
		}
		for j, input := range t.Input {
			modelList[i].Input[j] = &model.Payment{
				Address: input.Address,
				Amount:  input.Amount,
			}
		}
	}
	return modelList
}

// This function can be in the graph package or a utils package
func MapNewRecordToDBRecord(input model.NewRecord) (*db.Record, error) {
	var t time.Time
	var err error

	if input.Time != nil {
		t, err = ParseJSTimestampString(*input.Time)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time: %w", err)
		}
	} else {
		t = time.Now()
	}

	record := &db.Record{
		RecordInfo: db.RecordInfo{
			// ID will be set separately for create vs update
			Name:          input.Name,
			Amount:        input.Amount,
			Time:          t,
			PrePayAddress: db.Address(input.PrePayAddress),
			Category:      db.RecordCategory(RecordCategory2Int(input.Category)),
		},
		RecordData: db.RecordData{
			ShouldPayAddress: make([]db.ExtendAddress, len(input.ShouldPayAddress)),
		},
	}

	for i, addr := range input.ShouldPayAddress {
		if i < len(input.ExtendPayMsg) {
			record.ShouldPayAddress[i] = db.ExtendAddress{
				Address:   db.Address(addr),
				ExtendMsg: input.ExtendPayMsg[i],
			}
		} else {
			record.ShouldPayAddress[i] = db.ExtendAddress{
				Address:   db.Address(addr),
				ExtendMsg: 0, // Default to 0 if ExtendPayMsg is not provided
			}
		}
	}

	return record, nil
}
