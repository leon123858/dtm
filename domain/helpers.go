package domain

import (
	"slices"
	"strconv"

	odiff "github.com/r3labs/diff/v3"
)

func (r Record) EditableFields() RecordFields {
	fields := RecordFields{
		Name: r.Name, Amount: r.Amount, Time: strconv.FormatInt(r.Time.UnixMilli(), 10),
		PrePayAddressID: r.PrePayAddress.ID.String(), Category: strconv.Itoa(int(r.Category)),
		IsDeleted: r.IsDeleted, ShouldPayAddress: make(RecordShares, len(r.ShouldPayAddress)),
	}
	for i, address := range r.ShouldPayAddress {
		fields.ShouldPayAddress[i] = RecordShare{AddressID: address.Address.ID.String(), ExtendMsg: address.ExtendMsg}
	}
	return fields
}

func (p RecordPatch) Clone() RecordPatch {
	copy := RecordPatch{Changes: make(odiff.Changelog, len(p.Changes))}
	for i, change := range p.Changes {
		// Scalar and RecordShare values contain no references.
		copy.Changes[i] = odiff.Change{Type: change.Type, Path: slices.Clone(change.Path), From: change.From, To: change.To}
	}
	return copy
}
