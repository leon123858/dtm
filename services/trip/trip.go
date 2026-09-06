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

func (f *tripFactory) ForTrip(id uuid.UUID, options ...ReadOptions) Trip {
	var readOptions ReadOptions
	if len(options) > 0 {
		readOptions = options[0]
	}
	return &trip{id: id, store: f.store, readers: f.readers, policy: f.policy, readOptions: readOptions}
}

type trip struct {
	readOptions ReadOptions
	id          uuid.UUID
	store       db.TripStore
	readers     db.ReaderProvider
	policy      recordPolicy
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
		result := newMaterializedRecord(t.id, materialized)
		return AppendResult{TripID: t.id, Record: result, Appended: true}, nil
	case intentPatch:
		tripID, materialized, appended, err := t.store.AppendPatch(ctx, t.id, r.targetID, r.patch, t.policy)
		if err != nil {
			return AppendResult{}, fromStoreError(err)
		}
		result := newMaterializedRecord(tripID, materialized)
		return AppendResult{TripID: tripID, Record: result, Appended: appended}, nil
	default:
		return AppendResult{TripID: t.id, Record: value, Appended: false}, nil
	}
}

type recordProjection struct {
	records []db.RecordSnapshot
	tails   []db.RecordSnapshot
	active  map[uuid.UUID]bool
}

// loadProjection reads through the DataLoader and derives the requested view.
func (t *trip) loadProjection(ctx context.Context) (recordProjection, error) {
	if err := ctx.Err(); err != nil {
		return recordProjection{}, err
	}

	reader, err := t.reader(ctx)
	if err != nil {
		return recordProjection{}, err
	}
	records, err := reader.LoadTripRecords(ctx, t.id, db.RecordReadOptions{HaveHistory: t.readOptions.HaveHistory})
	if err != nil {
		return recordProjection{}, fromStoreError(err)
	}
	projection := recordProjection{records: records, active: make(map[uuid.UUID]bool, len(records))}
	if t.readOptions.HaveHistory {
		chains, err := scanChains(ctx, records)
		if err != nil {
			return recordProjection{}, err
		}
		projection.active = activeTailIDs(chains)
		byID := make(map[uuid.UUID]db.RecordSnapshot, len(records))
		for _, record := range records {
			byID[record.ID] = record
		}
		for _, chain := range chains {
			if len(chain) == 0 {
				continue
			}
			tail := byID[chain[len(chain)-1].ID]
			if !tail.IsDeleted {
				projection.tails = append(projection.tails, tail)
			}
		}
	} else {
		for _, record := range records {
			projection.active[record.ID] = true
		}
		projection.tails = records
	}

	return projection, nil
}

func (t *trip) List(ctx context.Context) ([]Record, error) {
	projection, err := t.loadProjection(ctx)
	if err != nil {
		return nil, fmt.Errorf("load records for trip %s: %w", t.id, err)
	}
	result := make([]Record, len(projection.records))
	for i, value := range projection.records {
		result[i] = &record{tripID: t.id, data: cloneDomainRecord(value.Record), active: projection.active[value.ID]}
	}
	return result, nil
}

func (t *trip) CalculateMoneyShare(ctx context.Context) (MoneyShareResult, error) {
	projection, err := t.loadProjection(ctx)
	if err != nil {
		return MoneyShareResult{}, err
	}
	payments := make([]tx.UserPayment, 0, len(projection.tails))
	for _, tail := range projection.tails {
		if validateErr := t.policy.Validate(tail.Record); validateErr != nil {
			if errors.Is(validateErr, ErrInvalidRecordSnapshot) {
				return MoneyShareResult{Valid: false}, nil
			}
			return MoneyShareResult{}, validateErr
		}
		payments = append(payments, paymentFromRecord(tail.RecordInfo, tail.ShouldPayAddress))
	}

	pkg, remaining, err := tx.ShareMoneyEasy(payments)
	if err != nil {
		return MoneyShareResult{Valid: false}, nil
	}
	return MoneyShareResult{Package: pkg, TotalRemaining: remaining, Valid: true}, nil
}

func newMaterializedRecord(tripID uuid.UUID, value domain.Record) Record {
	return &record{tripID: tripID, data: cloneDomainRecord(value), active: true}
}
