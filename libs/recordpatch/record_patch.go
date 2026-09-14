// Package recordpatch generates and applies record changelogs with r3labs/diff.
// Callers apply patches inside the database append transaction or lock.
package recordpatch

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"dtm/domain"
	"dtm/libs/diff"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
)

// Diff generates the patch from the client's baseline, never from
// the current database tail. The latter is only read while applying the patch.
// Callers validate the new snapshot, including member uniqueness, before Diff.
func Diff(old, next domain.RecordFields) (domain.RecordPatch, error) {
	// Empty and nil collections have the same record meaning.
	if len(old.ShouldPayAddress) == 0 {
		old.ShouldPayAddress = nil
	}
	if len(next.ShouldPayAddress) == 0 {
		next.ShouldPayAddress = nil
	}
	changes, err := diff.GetCustomDiffer().Diff(old, next)
	if err != nil {
		return domain.RecordPatch{}, fmt.Errorf("diff record: %w", err)
	}
	return (domain.RecordPatch{Changes: changes}).Clone(), nil
}

// Apply accepts a patch produced by Diff and applies it to a detached snapshot.
// The caller enforces record policy before persisting inside the append lock.
func Apply(tail domain.Record, p domain.RecordPatch) (domain.Record, error) {
	fields := tail.EditableFields()
	differ := diff.GetCustomDiffer()
	for _, change := range p.Clone().Changes {
		// r3labs v3.0.2 clears the entire slice when deleting a missing member.
		if share, ok := change.From.(domain.RecordShare); ok && change.Type == odiff.DELETE &&
			!slices.ContainsFunc(fields.ShouldPayAddress, func(s domain.RecordShare) bool { return s.AddressID == share.AddressID }) {
			continue
		}
		for _, entry := range differ.Patch(odiff.Changelog{change}, &fields) {
			if entry.Errors != nil {
				return domain.Record{}, fmt.Errorf("apply record patch at %v: %w", entry.Path, entry.Errors)
			}
		}
	}
	return materialize(fields, tail)
}

func materialize(fields domain.RecordFields, tail domain.Record) (domain.Record, error) {
	result := tail
	if tail.ParentRecordID != nil {
		id := *tail.ParentRecordID
		result.ParentRecordID = &id
	}
	if tail.ChildRecordID != nil {
		id := *tail.ChildRecordID
		result.ChildRecordID = &id
	}
	result.Name, result.Amount, result.IsDeleted = fields.Name, fields.Amount, fields.IsDeleted
	category, err := strconv.Atoi(fields.Category)
	if err != nil {
		return domain.Record{}, fmt.Errorf("invalid patched category: %w", err)
	}
	result.Category = domain.RecordCategory(category)
	// Preserve the tail's original precision and location if Time was untouched.
	if fields.Time != strconv.FormatInt(tail.Time.UnixMilli(), 10) {
		millis, err := strconv.ParseInt(fields.Time, 10, 64)
		if err != nil {
			return domain.Record{}, fmt.Errorf("invalid patched time: %w", err)
		}
		result.Time = time.UnixMilli(millis)
	}
	payerID, err := uuid.Parse(fields.PrePayAddressID)
	if err != nil {
		return domain.Record{}, fmt.Errorf("invalid patched pre-pay address: %w", err)
	}
	// Retain known display names; the business policy resolves all addresses
	// against the trip after patching, including newly introduced addresses.
	addresses := map[uuid.UUID]domain.Address{tail.PrePayAddress.ID: tail.PrePayAddress}
	for _, address := range tail.ShouldPayAddress {
		addresses[address.Address.ID] = address.Address
	}
	addressFor := func(id uuid.UUID) domain.Address {
		if address, ok := addresses[id]; ok {
			return address
		}
		return domain.Address{ID: id}
	}
	result.PrePayAddress = addressFor(payerID)
	result.ShouldPayAddress = make([]domain.ExtendAddress, len(fields.ShouldPayAddress))
	for i, share := range fields.ShouldPayAddress {
		id, err := uuid.Parse(share.AddressID)
		if err != nil {
			return domain.Record{}, fmt.Errorf("invalid patched should-pay address at index %d: %w", i, err)
		}
		result.ShouldPayAddress[i] = domain.ExtendAddress{Address: addressFor(id), ExtendMsg: share.ExtendMsg}
	}
	return result, nil
}
