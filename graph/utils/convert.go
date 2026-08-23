package utils

import (
	"dtm/db/db"
	"dtm/domain"
	"dtm/graph/model"

	"dtm/tx"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	RecordCategoryList = []model.RecordCategory{
		model.RecordCategoryNormal,
		model.RecordCategoryFix,
		model.RecordCategoryPart,
		model.RecordCategoryFixBeforeNormal,
		model.RecordCategoryTransfer,
	}
	Category2IntMap = map[model.RecordCategory]int{}
	Int2CategoryMap = map[int]model.RecordCategory{}
)

func init() {
	for i, category := range RecordCategoryList {
		Category2IntMap[category] = i
		Int2CategoryMap[i] = category
	}
}

func RecordCategory2Int(category *model.RecordCategory) int {
	if val, ok := Category2IntMap[*category]; ok {
		return val
	}
	panic("unknown RecordCategory to int: " + fmt.Sprintf("%v", category))
}

func Int2RecordCategory(categoryInt int) model.RecordCategory {
	if val, ok := Int2CategoryMap[categoryInt]; ok {
		return val
	}
	panic("unknown RecordCategory: " + fmt.Sprintf("%v", categoryInt))
}

func ToModelTxList(txList []tx.Tx) []*model.Tx {
	modelList := make([]*model.Tx, len(txList))
	for i, t := range txList {
		modelList[i] = &model.Tx{
			Input: make([]*model.Payment, len(t.Input)),
			Output: &model.Payment{
				Address: ToModelAddress(t.Output.Address),
				Amount:  t.Output.Amount,
			},
		}
		for j, input := range t.Input {
			modelList[i].Input[j] = &model.Payment{
				Address: ToModelAddress(input.Address),
				Amount:  input.Amount,
			}
		}
	}
	return modelList
}

func ToModelAddress(address domain.Address) *model.Address {
	return &model.Address{ID: address.ID.String(), Name: address.Name}
}

func ToDomainAddress(address *model.Address) (domain.Address, error) {
	if address == nil {
		return domain.Address{}, fmt.Errorf("address is nil")
	}
	id, err := uuid.Parse(address.ID)
	if err != nil {
		return domain.Address{}, fmt.Errorf("invalid address ID: %w", err)
	}
	return domain.Address{ID: id, Name: address.Name}, nil
}

func CanonicalizeRecordAddresses(wrapper db.TripDBWrapper, tripID uuid.UUID, record *domain.Record) error {
	addresses, err := wrapper.GetTripAddressList(tripID)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]domain.Address, len(addresses))
	for _, address := range addresses {
		byID[address.ID] = address
	}
	prePay, ok := byID[record.PrePayAddress.ID]
	if !ok {
		return fmt.Errorf("pre-pay address does not belong to trip")
	}
	record.PrePayAddress = prePay
	for i := range record.ShouldPayAddress {
		address, ok := byID[record.ShouldPayAddress[i].Address.ID]
		if !ok {
			return fmt.Errorf("should-pay address at index %d does not belong to trip", i)
		}
		record.ShouldPayAddress[i].Address = address
	}
	return nil
}

// MapNewRecordToDomainRecord converts GraphQL input into a domain record.
func MapNewRecordToDomainRecord(input model.NewRecord) (*domain.Record, error) {
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

	prePayAddressID, err := uuid.Parse(input.PrePayAddressID)
	if err != nil {
		return nil, fmt.Errorf("invalid pre-pay address ID: %w", err)
	}
	record := &domain.Record{
		RecordInfo: domain.RecordInfo{
			// ID will be set separately for create vs update
			Name:          input.Name,
			Amount:        input.Amount,
			Time:          t,
			PrePayAddress: domain.Address{ID: prePayAddressID},
			Category:      domain.RecordCategory(RecordCategory2Int(input.Category)),
		},
		RecordData: domain.RecordData{
			ShouldPayAddress: make([]domain.ExtendAddress, len(input.ShouldPayAddressIds)),
		},
	}

	for i, rawID := range input.ShouldPayAddressIds {
		addressID, err := uuid.Parse(rawID)
		if err != nil {
			return nil, fmt.Errorf("invalid should-pay address ID at index %d: %w", i, err)
		}
		if i < len(input.ExtendPayMsg) {
			record.ShouldPayAddress[i] = domain.ExtendAddress{
				Address:   domain.Address{ID: addressID},
				ExtendMsg: input.ExtendPayMsg[i],
			}
		} else {
			record.ShouldPayAddress[i] = domain.ExtendAddress{
				Address:   domain.Address{ID: addressID},
				ExtendMsg: 0, // Default to 0 if ExtendPayMsg is not provided
			}
		}
	}

	return record, nil
}
