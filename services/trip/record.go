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
	tripID     uuid.UUID
	data       domain.Record
	active     bool
	eventValid bool
	readers    db.ReaderProvider
	intent     intentKind
	targetID   uuid.UUID
	patch      domain.RecordPatch
}

var _ Record = (*record)(nil)

func (r *record) ID() uuid.UUID               { return r.data.ID }
func (r *record) TripID() uuid.UUID           { return r.tripID }
func (r *record) Info() domain.RecordInfo     { return cloneRecordInfo(r.data.RecordInfo) }
func (r *record) DomainRecord() domain.Record { return cloneDomainRecord(r.data) }
func (r *record) IsActive() bool              { return r.active }
func (r *record) EventValid() bool            { return r.eventValid }

func (r *record) GetShouldPay(ctx context.Context) ([]domain.ExtendAddress, error) {
	if r.data.ShouldPayAddress != nil {
		return cloneAddresses(r.data.ShouldPayAddress), nil
	}
	reader, err := readerFrom(ctx, r.readers)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if reader == nil {
		return nil, nil
	}
	addresses, err := reader.LoadRecordShouldPay(ctx, r.data.ID)
	if err != nil {
		return nil, fmt.Errorf("load should-pay addresses for record %s: %w", r.data.ID, fromStoreError(err))
	}
	return cloneAddresses(addresses), nil
}

func (r *record) Validate(ctx context.Context) (bool, error) {
	if !r.eventValid {
		return false, nil
	}
	value := r.DomainRecord()
	addresses, err := r.GetShouldPay(ctx)
	if err != nil {
		return false, err
	}
	value.ShouldPayAddress = addresses
	if err := (recordPolicy{}).Validate(value); err != nil {
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
	return &record{tripID: tripID, data: value, active: true, eventValid: true, readers: f.readers, intent: intentCreate}, nil
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
	return &record{tripID: node.TripID, readers: f.readers, intent: intentPatch, targetID: recordID, patch: patch.Clone()}, nil
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
	return &record{tripID: node.TripID, data: domain.Record{RecordInfo: cloneRecordInfo(node.Info)}, active: node.Info.ChildRecordID == nil, eventValid: eventShapeValid(node.Info), readers: f.readers}, nil
}

func (f *recordFactory) FromInfo(info domain.RecordInfo, eventValid bool) Record {
	return &record{data: domain.Record{RecordInfo: cloneRecordInfo(info)}, active: info.ChildRecordID == nil, eventValid: eventValid, readers: f.readers}
}

func readerFrom(ctx context.Context, provider db.ReaderProvider) (db.Reader, error) {
	if provider == nil {
		return nil, nil
	}
	return provider(ctx)
}
