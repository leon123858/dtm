package utils

import (
	"dtm/domain"
	"dtm/graph/model"
	"dtm/services/tx"
	"fmt"
	"reflect"
	"strconv"
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
	category, err := Int2RecordCategoryChecked(categoryInt)
	if err != nil {
		panic(err)
	}
	return category
}

func Int2RecordCategoryChecked(categoryInt int) (model.RecordCategory, error) {
	if val, ok := Int2CategoryMap[categoryInt]; ok {
		return val, nil
	}
	return "", fmt.Errorf("unknown RecordCategory: %d", categoryInt)
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

func ToModelRecord(info domain.RecordInfo, active bool) *model.Record {
	result, err := ToModelRecordChecked(info, active, true)
	if err != nil {
		panic(err)
	}
	return result
}

func ToModelRecordChecked(info domain.RecordInfo, active, eventValid bool) (*model.Record, error) {
	category, err := Int2RecordCategoryChecked(int(info.Category))
	if err != nil {
		return nil, fmt.Errorf("record %s: %w", info.ID, err)
	}
	result := &model.Record{
		ID: info.ID.String(), Time: strconv.FormatInt(info.Time.UnixMilli(), 10),
		Category: category, IsDeleted: info.IsDeleted, IsActive: active, EventValid: eventValid,
	}
	if info.ParentRecordID != nil {
		parent := info.ParentRecordID.String()
		result.ParentRecordID = &parent
	}
	result.Name = &info.Name
	result.Amount = &info.Amount
	result.PrePayAddress = ToModelAddress(info.PrePayAddress)
	return result, nil
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

func ModelRecordInfo(record *model.Record) (domain.RecordInfo, error) {
	id, err := uuid.Parse(record.ID)
	if err != nil {
		return domain.RecordInfo{}, fmt.Errorf("invalid record ID: %w", err)
	}
	info := domain.RecordInfo{ID: id, Category: domain.RecordCategory(RecordCategory2Int(&record.Category)), IsDeleted: record.IsDeleted}
	if record.ParentRecordID != nil {
		parentID, parseErr := uuid.Parse(*record.ParentRecordID)
		if parseErr != nil {
			return domain.RecordInfo{}, fmt.Errorf("invalid parent record ID: %w", parseErr)
		}
		info.ParentRecordID = &parentID
	}
	if record.Name != nil {
		info.Name = *record.Name
	}
	if record.Amount != nil {
		info.Amount = *record.Amount
	}
	if record.PrePayAddress != nil {
		info.PrePayAddress, err = ToDomainAddress(record.PrePayAddress)
		if err != nil {
			return domain.RecordInfo{}, err
		}
	}
	return info, nil
}

func BuildRecordPatch(oldInput, newInput model.NewRecord, newRecord *domain.Record) domain.RecordPatch {
	var patch domain.RecordPatch
	if oldInput.Name != newInput.Name {
		patch.Name = &newRecord.Name
	}
	if oldInput.Amount != newInput.Amount {
		patch.Amount = &newRecord.Amount
	}
	if oldInput.PrePayAddressID != newInput.PrePayAddressID {
		patch.PrePayAddressID = &newRecord.PrePayAddress.ID
	}
	if *oldInput.Category != *newInput.Category {
		patch.Category = &newRecord.Category
	}
	if newInput.Time != nil && (oldInput.Time == nil || *oldInput.Time != *newInput.Time) {
		patch.Time = &newRecord.Time
	}
	if !reflect.DeepEqual(oldInput.ShouldPayAddressIds, newInput.ShouldPayAddressIds) ||
		!reflect.DeepEqual(oldInput.ExtendPayMsg, newInput.ExtendPayMsg) {
		addresses := append([]domain.ExtendAddress(nil), newRecord.ShouldPayAddress...)
		patch.ShouldPayAddress = &addresses
	}
	// Omission means inherit the transaction-time tail. An explicit value only
	// becomes a patch when it differs from the client's old baseline.
	if newInput.IsDeleted != nil {
		oldDeleted := oldInput.IsDeleted != nil && *oldInput.IsDeleted
		if oldDeleted != *newInput.IsDeleted {
			patch.IsDeleted = newInput.IsDeleted
		}
	}
	return patch
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
			IsDeleted:     input.IsDeleted != nil && *input.IsDeleted,
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
