package graph

import (
	"fmt"
	"reflect"

	"dtm/domain"
	"dtm/graph/model"
	"dtm/graph/utils"

	"github.com/google/uuid"
)

func buildRecordPatch(oldInput, newInput model.NewRecord, newRecord *domain.Record) domain.RecordPatch {
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

func modelRecordInfo(record *model.Record) (domain.RecordInfo, error) {
	id, err := uuid.Parse(record.ID)
	if err != nil {
		return domain.RecordInfo{}, fmt.Errorf("invalid record ID: %w", err)
	}
	info := domain.RecordInfo{ID: id, Category: domain.RecordCategory(utils.RecordCategory2Int(&record.Category)), IsDeleted: record.IsDeleted}
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
		info.PrePayAddress, err = utils.ToDomainAddress(record.PrePayAddress)
		if err != nil {
			return domain.RecordInfo{}, err
		}
	}
	return info, nil
}
