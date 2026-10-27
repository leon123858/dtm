package utils

import "dtm/graph/model"

func ToAddressSnapshot(address *model.Address) *model.AddressSnapshot {
	if address == nil {
		return nil
	}
	return &model.AddressSnapshot{ID: address.ID, Name: address.Name, UseNickname: address.UseNickname}
}

func ToRecordSnapshot(record *model.Record) *model.RecordSnapshot {
	if record == nil {
		return nil
	}
	var addresses []*model.AddressSnapshot
	if record.ShouldPayAddress != nil {
		addresses = make([]*model.AddressSnapshot, len(record.ShouldPayAddress))
		for i, address := range record.ShouldPayAddress {
			addresses[i] = ToAddressSnapshot(address)
		}
	}
	return &model.RecordSnapshot{
		ID: record.ID, Name: record.Name, Amount: record.Amount,
		Time: record.Time, PrePayAddress: ToAddressSnapshot(record.PrePayAddress),
		ShouldPayAddress: addresses, ExtendPayMsg: record.ExtendPayMsg,
		Category: record.Category, ParentRecordID: record.ParentRecordID,
		IsDeleted: record.IsDeleted, IsValid: record.IsValid, IsActive: record.IsActive,
	}
}
