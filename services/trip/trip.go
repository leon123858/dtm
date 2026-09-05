package trip

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"dtm/adapters/db/db"
	"dtm/domain"
	"dtm/services/tx"

	"github.com/google/uuid"
)

type tripFactory struct {
	store   db.TripStore
	readers db.ReaderProvider
	policy  recordPolicy
}

var _ TripFactory = (*tripFactory)(nil)

func (f *tripFactory) Create(ctx context.Context, name string) (Trip, error) {
	if err := ctx.Err(); err != nil {
		return nil, fromStoreError(err)
	}
	if f.store == nil {
		return nil, fmt.Errorf("trip store is not available")
	}
	id := uuid.New()
	info := &domain.TripInfo{ID: id, Name: name}
	if err := f.store.CreateTrip(info); err != nil {
		return nil, fromStoreError(err)
	}
	return &trip{id: id, store: f.store, readers: f.readers, policy: f.policy}, nil
}

func (f *tripFactory) ForTrip(id uuid.UUID) Trip {
	return &trip{id: id, store: f.store, readers: f.readers, policy: f.policy}
}

type trip struct {
	id      uuid.UUID
	store   db.TripStore
	readers db.ReaderProvider
	policy  recordPolicy
}

var _ Trip = (*trip)(nil)

func (t *trip) ID() uuid.UUID { return t.id }

func (t *trip) writable(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.store == nil {
		return fmt.Errorf("trip store is not available")
	}
	return nil
}

func (t *trip) reader(ctx context.Context) (db.Reader, error) {
	reader, err := readerFrom(ctx, t.readers)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if reader == nil {
		return nil, fmt.Errorf("chain reader is not available")
	}
	return reader, nil
}

func (t *trip) Info(ctx context.Context) (*domain.TripInfo, error) {
	reader, err := t.reader(ctx)
	if err != nil {
		return nil, fromStoreError(err)
	}
	info, err := reader.LoadTrip(ctx, t.id)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if info == nil {
		return nil, fmt.Errorf("%w: %s", ErrTripNotFound, t.id)
	}
	result := *info
	return &result, nil
}

func (t *trip) UpdateInfo(ctx context.Context, name string) (*domain.TripInfo, error) {
	if err := t.writable(ctx); err != nil {
		return nil, fromStoreError(err)
	}
	info := &domain.TripInfo{ID: t.id, Name: name}
	if err := t.store.UpdateTripInfo(info); err != nil {
		return nil, fromStoreError(err)
	}
	result := *info
	return &result, nil
}

func (t *trip) Addresses(ctx context.Context) ([]domain.Address, error) {
	reader, err := t.reader(ctx)
	if err != nil {
		return nil, fromStoreError(err)
	}
	addresses, err := reader.LoadTripAddresses(ctx, t.id)
	if err != nil {
		return nil, fromStoreError(err)
	}
	return slices.Clone(addresses), nil
}

func (t *trip) CreateAddress(ctx context.Context, name string) (*domain.Address, error) {
	if err := t.writable(ctx); err != nil {
		return nil, fromStoreError(err)
	}
	address, err := t.store.CreateAddress(t.id, name)
	return cloneAddress(address), fromStoreError(err)
}

func (t *trip) UpdateAddress(ctx context.Context, addressID uuid.UUID, name string) (*domain.Address, error) {
	if err := t.writable(ctx); err != nil {
		return nil, fromStoreError(err)
	}
	address, err := t.store.UpdateAddress(t.id, addressID, name)
	return cloneAddress(address), fromStoreError(err)
}

func (t *trip) DeleteAddress(ctx context.Context, addressID uuid.UUID) (*domain.Address, error) {
	if err := t.writable(ctx); err != nil {
		return nil, fromStoreError(err)
	}
	address, err := t.store.DeleteAddress(t.id, addressID)
	return cloneAddress(address), fromStoreError(err)
}

func cloneAddress(address *domain.Address) *domain.Address {
	if address == nil {
		return nil
	}
	result := *address
	return &result
}

func (t *trip) Append(ctx context.Context, value Record) (AppendResult, error) {
	r, ok := value.(*record)
	if !ok || r == nil {
		return AppendResult{}, fmt.Errorf("%w: unsupported record implementation", ErrInvalidRecordSnapshot)
	}
	if r.tripID != t.id {
		return AppendResult{}, fmt.Errorf("%w: record trip %s does not match trip %s", ErrInvalidRecordSnapshot, r.tripID, t.id)
	}
	if t.store == nil {
		return AppendResult{}, fmt.Errorf("chain store is not available")
	}
	switch r.intent {
	case intentCreate:
		materialized, err := t.store.AppendNew(ctx, t.id, r.DomainRecord(), t.policy)
		if err != nil {
			return AppendResult{}, fromStoreError(err)
		}
		result := newMaterializedRecord(t.id, materialized, t.readers)
		return AppendResult{TripID: t.id, Record: result, Appended: true}, nil
	case intentPatch:
		tripID, materialized, appended, err := t.store.AppendPatch(ctx, r.targetID, r.patch, t.policy)
		if err != nil {
			return AppendResult{}, fromStoreError(err)
		}
		result := newMaterializedRecord(tripID, materialized, t.readers)
		return AppendResult{TripID: tripID, Record: result, Appended: appended}, nil
	default:
		return AppendResult{TripID: t.id, Record: value, Appended: false}, nil
	}
}

func (t *trip) List(ctx context.Context) ([]Record, error) {
	reader, err := t.reader(ctx)
	if err != nil {
		return nil, fromStoreError(err)
	}
	infos, chains, err := loadChains(ctx, reader, t.id)
	if err != nil {
		return nil, fmt.Errorf("load records for trip %s: %w", t.id, err)
	}
	active := activeTailIDs(chains)
	result := make([]Record, len(infos))
	for i, info := range infos {
		result[i] = &record{tripID: t.id, data: domain.Record{RecordInfo: cloneRecordInfo(info)}, active: active[info.ID], eventValid: eventShapeValid(info), readers: t.readers}
	}
	return result, nil
}

func (t *trip) CalculateMoneyShare(ctx context.Context) (MoneyShareResult, error) {
	reader, err := t.reader(ctx)
	if err != nil {
		return MoneyShareResult{}, fromStoreError(err)
	}
	_, chains, err := loadChains(ctx, reader, t.id)
	if err != nil {
		return MoneyShareResult{}, fromStoreError(err)
	}
	payments := make([]tx.UserPayment, 0, len(chains))
	for _, c := range chains {
		if len(c) == 0 {
			continue
		}
		tail := c[len(c)-1].Value
		if tail.IsDeleted {
			continue
		}
		addresses, loadErr := reader.LoadRecordShouldPay(ctx, tail.ID)
		if loadErr != nil {
			return MoneyShareResult{}, fmt.Errorf("load should-pay addresses for record %s: %w", tail.ID, fromStoreError(loadErr))
		}
		value := domain.Record{RecordInfo: cloneRecordInfo(tail), RecordData: domain.RecordData{ShouldPayAddress: cloneAddresses(addresses)}}
		if validateErr := t.policy.Validate(value); validateErr != nil {
			if errors.Is(validateErr, ErrInvalidRecordSnapshot) {
				return MoneyShareResult{Valid: false}, nil
			}
			return MoneyShareResult{}, validateErr
		}
		payments = append(payments, paymentFromRecord(tail, addresses))
	}
	pkg, remaining, err := tx.ShareMoneyEasy(payments)
	if err != nil {
		return MoneyShareResult{Valid: false}, nil
	}
	return MoneyShareResult{Package: pkg, TotalRemaining: remaining, Valid: true}, nil
}

func newMaterializedRecord(tripID uuid.UUID, value domain.Record, readers db.ReaderProvider) Record {
	return &record{tripID: tripID, data: cloneDomainRecord(value), active: true, eventValid: eventShapeValid(value.RecordInfo), readers: readers}
}
