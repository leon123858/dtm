package trip

import (
	"context"
	"errors"
	"fmt"

	"dtm/adapters/db/db"
	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
)

type intentKind uint8

const (
	intentLoaded intentKind = iota
	intentCreate
	intentPatch
)

type record struct {
	tripID   uuid.UUID
	data     domain.Record
	active   bool
	intent   intentKind
	targetID uuid.UUID
	patch    domain.RecordPatch
}

var _ Record = (*record)(nil)

func (r *record) ID() uuid.UUID               { return r.data.ID }
func (r *record) TripID() uuid.UUID           { return r.tripID }
func (r *record) Info() domain.RecordInfo     { return cloneRecordInfo(r.data.RecordInfo) }
func (r *record) DomainRecord() domain.Record { return cloneDomainRecord(r.data) }
func (r *record) IsActive() bool              { return r.active }

func (r *record) GetShouldPay() []domain.ExtendAddress {
	return cloneAddresses(r.data.ShouldPayAddress)
}

func (r *record) Validate() (bool, error) {
	if err := (recordPolicy{}).Validate(r.data); err != nil {
		if errors.Is(err, ErrInvalidRecordSnapshot) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

type recordFactory struct{ readers db.ReaderProvider }

var _ RecordFactory = (*recordFactory)(nil)

func (f *recordFactory) New(_ context.Context, tripID uuid.UUID, value domain.Record) (Record, error) {
	value = cloneDomainRecord(value)
	value.ID = uuid.New()
	value.ParentRecordID = nil
	value.ChildRecordID = nil
	return &record{tripID: tripID, data: value, active: true, intent: intentCreate}, nil
}

func (f *recordFactory) Update(ctx context.Context, recordID uuid.UUID, patch domain.RecordPatch) (Record, error) {
	reader, err := readerFrom(ctx, f.readers)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if reader == nil {
		return nil, fmt.Errorf("record reader is not available")
	}
	node, err := reader.LoadRecord(ctx, recordID)
	if err != nil {
		if errors.Is(err, chainlist.ErrNodeNotFound) || errors.Is(err, db.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s: %w", ErrRecordNotFound, recordID, err)
		}
		return nil, fromStoreError(err)
	}
	return &record{tripID: node.TripID, intent: intentPatch, targetID: recordID, patch: patch.Clone()}, nil
}

func (f *recordFactory) ByID(ctx context.Context, recordID uuid.UUID) (Record, error) {
	reader, err := readerFrom(ctx, f.readers)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if reader == nil {
		return nil, fmt.Errorf("record reader is not available")
	}
	node, err := reader.LoadRecord(ctx, recordID)
	if err != nil {
		if errors.Is(err, chainlist.ErrNodeNotFound) || errors.Is(err, db.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s: %w", ErrRecordNotFound, recordID, err)
		}
		return nil, fromStoreError(err)
	}
	return &record{tripID: node.TripID, data: cloneDomainRecord(node.Record), active: node.ChildRecordID == nil}, nil
}

func (f *recordFactory) FromRecord(value domain.Record) Record {
	return &record{data: cloneDomainRecord(value), active: value.ChildRecordID == nil}
}

func readerFrom(ctx context.Context, provider db.ReaderProvider) (db.Reader, error) {
	if provider == nil {
		return nil, nil
	}
	return provider(ctx)
}
