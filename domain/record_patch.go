package domain

import (
	"slices"
	"strconv"

	odiff "github.com/r3labs/diff/v3"
)

// RecordFields is the common diff/patch target for client snapshots and the
// transaction-time tail. It deliberately excludes identity, links and names of
// addresses. IDs, milliseconds and category numbers use canonical strings so
// a malformed old client baseline can still be compared when repairing it.
type RecordFields struct {
	Name             string
	Amount           float64
	Time             string
	PrePayAddressID  string
	Category         string
	ShouldPayAddress RecordShares `diff:"ShouldPayAddress,omitunequal"`
	IsDeleted        bool
}

type RecordShare struct {
	AddressID string `diff:"AddressID,identifier"`
	ExtendMsg float64
}

// RecordShares identifies members by AddressID; order is not an edit.
type RecordShares []RecordShare

// RecordPatch contains an in-process r3labs changelog, not a serialized patch.
// Obtain patches from recordpatch.Diff; consumers trust its paths and value types.
// Changes target editable scalar fields or identified share members.
type RecordPatch struct {
	Changes odiff.Changelog
}

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
