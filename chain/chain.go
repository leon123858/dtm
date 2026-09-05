package chain

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"dtm/domain"
	"dtm/libs/chainlist"
	"dtm/tx"

	"github.com/google/uuid"
)

var ErrInvalidRecordSnapshot = errors.New("invalid record snapshot")

type TailResolutionError struct {
	TargetID uuid.UUID
	Err      error
}

func (e *TailResolutionError) Error() string {
	return fmt.Sprintf("resolve canonical tail for record %s: %v", e.TargetID, e.Err)
}
func (e *TailResolutionError) Unwrap() error { return e.Err }

type Store interface {
	AppendNew(context.Context, uuid.UUID, domain.Record) error
	AppendPatch(context.Context, uuid.UUID, domain.RecordPatch) (uuid.UUID, domain.Record, bool, error)
}

type RecordNode struct {
	TripID uuid.UUID
	Info   domain.RecordInfo
}

type Reader interface {
	LoadTrip(context.Context, uuid.UUID) (*domain.TripInfo, error)
	LoadRecord(context.Context, uuid.UUID) (RecordNode, error)
	LoadTripRecords(context.Context, uuid.UUID) ([]domain.RecordInfo, error)
	LoadTripAddresses(context.Context, uuid.UUID) ([]domain.Address, error)
	LoadRecordShouldPay(context.Context, uuid.UUID) ([]domain.ExtendAddress, error)
}

type InvalidRecordAddressesError struct{ Err error }

func (e *InvalidRecordAddressesError) Error() string {
	return fmt.Sprintf("invalid record addresses: %v", e.Err)
}
func (e *InvalidRecordAddressesError) Unwrap() error { return e.Err }

type intentKind uint8

const (
	intentLoaded intentKind = iota
	intentCreate
	intentPatch
)

type Record struct {
	tripID     uuid.UUID
	data       domain.Record
	active     bool
	eventValid bool
	reader     Reader
	intent     intentKind
	targetID   uuid.UUID
	patch      domain.RecordPatch
}

func (r Record) ID() uuid.UUID               { return r.data.ID }
func (r Record) TripID() uuid.UUID           { return r.tripID }
func (r Record) Info() domain.RecordInfo     { return cloneRecordInfo(r.data.RecordInfo) }
func (r Record) DomainRecord() domain.Record { return cloneDomainRecord(r.data) }
func (r Record) IsActive() bool              { return r.active }
func (r Record) EventValid() bool            { return r.eventValid }

func (r Record) GetShouldPay(ctx context.Context) ([]domain.ExtendAddress, error) {
	if r.data.ShouldPayAddress != nil {
		return cloneAddresses(r.data.ShouldPayAddress), nil
	}
	if r.reader == nil {
		return nil, nil
	}
	addresses, err := r.reader.LoadRecordShouldPay(ctx, r.data.ID)
	if err != nil {
		return nil, fmt.Errorf("load should-pay addresses for record %s: %w", r.data.ID, err)
	}
	return cloneAddresses(addresses), nil
}

func (r Record) Validate(ctx context.Context) (bool, error) {
	if !r.eventValid || r.data.ID == uuid.Nil || r.data.Category < domain.CategoryNormal || r.data.Category > domain.CategoryTransfer {
		return false, nil
	}
	addresses, err := r.GetShouldPay(ctx)
	if err != nil {
		return false, err
	}
	payment := paymentFromRecord(r.data.RecordInfo, addresses)
	transaction, err := payment.ToTx(tx.ShareMoneyStrategyFactory(payment.PaymentType))
	if err != nil {
		return false, nil
	}
	return transaction.BoolValidate(), nil
}

type RecordFactory struct {
	store  Store
	reader Reader
}

func NewRecordFactory(store Store, reader Reader) *RecordFactory {
	return &RecordFactory{store: store, reader: reader}
}

func (f *RecordFactory) New(ctx context.Context, tripID uuid.UUID, value domain.Record) (Record, error) {
	value = cloneDomainRecord(value)
	value.ID = uuid.New()
	value.ParentRecordID = nil
	value.ChildRecordID = nil
	var err error
	value, err = f.Canonicalize(ctx, tripID, value)
	if err != nil {
		return Record{}, err
	}
	return Record{tripID: tripID, data: value, active: true, eventValid: true, reader: f.reader, intent: intentCreate}, nil
}

// Canonicalize validates that every address belongs to tripID and replaces
// client display data with the trip's canonical address values.
func (f *RecordFactory) Canonicalize(ctx context.Context, tripID uuid.UUID, value domain.Record) (domain.Record, error) {
	value = cloneDomainRecord(value)
	if f.reader == nil {
		return value, nil
	}
	addresses, err := f.reader.LoadTripAddresses(ctx, tripID)
	if err != nil {
		return domain.Record{}, &InvalidRecordAddressesError{Err: fmt.Errorf("load addresses for trip %s: %w", tripID, err)}
	}
	if err := canonicalizeRecordAddresses(addresses, &value); err != nil {
		return domain.Record{}, &InvalidRecordAddressesError{Err: err}
	}
	return value, nil
}

// Update resolves only the target and owning trip. The store resolves and
// merges the transaction-time canonical tail while holding its chain lock.
func (f *RecordFactory) Update(ctx context.Context, recordID uuid.UUID, patch domain.RecordPatch) (Record, error) {
	if f.reader == nil {
		return Record{}, fmt.Errorf("record reader is not available")
	}
	node, err := f.reader.LoadRecord(ctx, recordID)
	if err != nil {
		if errors.Is(err, chainlist.ErrNodeNotFound) {
			return Record{}, &TailResolutionError{TargetID: recordID, Err: err}
		}
		return Record{}, err
	}
	return Record{tripID: node.TripID, reader: f.reader, intent: intentPatch, targetID: recordID, patch: clonePatch(patch)}, nil
}

func (f *RecordFactory) ByID(ctx context.Context, recordID uuid.UUID) (Record, error) {
	node, err := f.reader.LoadRecord(ctx, recordID)
	if err != nil {
		return Record{}, err
	}
	return Record{tripID: node.TripID, data: domain.Record{RecordInfo: cloneRecordInfo(node.Info)}, active: node.Info.ChildRecordID == nil, eventValid: eventShapeValid(node.Info), reader: f.reader}, nil
}

func (f *RecordFactory) FromInfo(info domain.RecordInfo, eventValid bool) Record {
	return Record{data: domain.Record{RecordInfo: cloneRecordInfo(info)}, active: info.ChildRecordID == nil, eventValid: eventValid, reader: f.reader}
}

type AppendResult struct {
	TripID   uuid.UUID
	Record   Record
	Appended bool
}

type MoneyShareResult struct {
	Package        tx.Package
	TotalRemaining float64
	Valid          bool
}

type Trip struct {
	id     uuid.UUID
	store  Store
	reader Reader
}

func NewTrip(tripID uuid.UUID, store Store, reader Reader) *Trip {
	return &Trip{id: tripID, store: store, reader: reader}
}

func (t *Trip) Info(ctx context.Context) (*domain.TripInfo, error) {
	info, err := t.reader.LoadTrip(ctx, t.id)
	if err != nil || info == nil {
		return info, err
	}
	result := *info
	return &result, nil
}

func (t *Trip) Addresses(ctx context.Context) ([]domain.Address, error) {
	addresses, err := t.reader.LoadTripAddresses(ctx, t.id)
	if err != nil {
		return nil, err
	}
	return slices.Clone(addresses), nil
}

func (t *Trip) Append(ctx context.Context, record Record) (AppendResult, error) {
	if record.tripID != t.id {
		return AppendResult{}, fmt.Errorf("record trip %s does not match trip %s", record.tripID, t.id)
	}
	switch record.intent {
	case intentCreate:
		if err := t.store.AppendNew(ctx, t.id, record.DomainRecord()); err != nil {
			return AppendResult{}, err
		}
		return AppendResult{TripID: t.id, Record: record, Appended: true}, nil
	case intentPatch:
		tripID, materialized, appended, err := t.store.AppendPatch(ctx, record.targetID, record.patch)
		if err != nil {
			return AppendResult{}, err
		}
		result := Record{tripID: tripID, data: materialized, active: true, eventValid: eventShapeValid(materialized.RecordInfo), reader: record.reader}
		return AppendResult{TripID: tripID, Record: result, Appended: appended}, nil
	default:
		return AppendResult{TripID: t.id, Record: record, Appended: false}, nil
	}
}

func (t *Trip) List(ctx context.Context) ([]Record, error) {
	infos, chains, err := t.chains(ctx)
	if err != nil {
		return nil, fmt.Errorf("load records for trip %s: %w", t.id, err)
	}
	active := make(map[uuid.UUID]bool, len(chains))
	for _, c := range chains {
		if len(c) > 0 {
			active[c[len(c)-1].ID] = true
		}
	}
	result := make([]Record, len(infos))
	for i, info := range infos {
		result[i] = Record{tripID: t.id, data: domain.Record{RecordInfo: info}, active: active[info.ID], eventValid: eventShapeValid(info), reader: t.reader}
	}
	return result, nil
}

func (t *Trip) CalculateMoneyShare(ctx context.Context) (MoneyShareResult, error) {
	_, chains, err := t.chains(ctx)
	if err != nil {
		return MoneyShareResult{}, err
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
		addresses, loadErr := t.reader.LoadRecordShouldPay(ctx, tail.ID)
		if loadErr != nil {
			return MoneyShareResult{}, fmt.Errorf("load should-pay addresses for record %s: %w", tail.ID, loadErr)
		}
		payment := paymentFromRecord(tail, addresses)
		transaction, txErr := payment.ToTx(tx.ShareMoneyStrategyFactory(payment.PaymentType))
		if txErr != nil || !transaction.BoolValidate() {
			return MoneyShareResult{Valid: false}, nil
		}
		payments = append(payments, payment)
	}
	pkg, remaining, err := tx.ShareMoneyEasy(payments)
	if err != nil {
		return MoneyShareResult{Valid: false}, nil
	}
	return MoneyShareResult{Package: pkg, TotalRemaining: remaining, Valid: true}, nil
}

func (t *Trip) chains(ctx context.Context) ([]domain.RecordInfo, [][]chainlist.Node[uuid.UUID, domain.RecordInfo], error) {
	infos, err := t.reader.LoadTripRecords(ctx, t.id)
	if err != nil {
		return nil, nil, err
	}
	nodes := make([]chainlist.Node[uuid.UUID, domain.RecordInfo], len(infos))
	order := make(map[uuid.UUID]int, len(infos))
	for i, info := range infos {
		nodes[i] = InfoNode(info)
		if _, ok := order[info.ID]; !ok {
			order[info.ID] = i
		}
	}
	source, err := chainlist.NewMemorySource(nodes)
	if err != nil {
		return nil, nil, err
	}
	var chains [][]chainlist.Node[uuid.UUID, domain.RecordInfo]
	for c, walkErr := range source.Chains(ctx, func(a, b chainlist.Node[uuid.UUID, domain.RecordInfo]) int { return order[a.ID] - order[b.ID] }) {
		if walkErr != nil {
			return nil, nil, walkErr
		}
		chains = append(chains, c)
	}
	return infos, chains, nil
}

func InfoNode(info domain.RecordInfo) chainlist.Node[uuid.UUID, domain.RecordInfo] {
	return chainlist.Node[uuid.UUID, domain.RecordInfo]{ID: info.ID, ParentID: info.ParentRecordID, ChildID: info.ChildRecordID, Value: info}
}

func eventShapeValid(info domain.RecordInfo) bool {
	return info.ID != uuid.Nil && info.Category >= domain.CategoryNormal && info.Category <= domain.CategoryTransfer
}

// MergeRecordPatch applies a patch to the transaction-time tail, canonicalizes
// address display values, and detects a materialized no-op. It intentionally
// does not validate payment semantics.
func MergeRecordPatch(tail domain.Record, patch domain.RecordPatch, tripAddresses []domain.Address) (domain.Record, bool, error) {
	result := cloneDomainRecord(tail)
	if patch.Name != nil {
		result.Name = *patch.Name
	}
	if patch.Amount != nil {
		result.Amount = *patch.Amount
	}
	if patch.Time != nil {
		result.Time = *patch.Time
	}
	if patch.PrePayAddressID != nil {
		result.PrePayAddress = domain.Address{ID: *patch.PrePayAddressID}
	}
	if patch.Category != nil {
		result.Category = *patch.Category
	}
	if patch.ShouldPayAddress != nil {
		result.ShouldPayAddress = cloneAddresses(*patch.ShouldPayAddress)
	}
	if patch.IsDeleted != nil {
		result.IsDeleted = *patch.IsDeleted
	}
	if err := canonicalizeRecordAddresses(tripAddresses, &result); err != nil {
		return domain.Record{}, false, &InvalidRecordAddressesError{Err: err}
	}
	return result, !reflect.DeepEqual(result, tail), nil
}

func canonicalizeRecordAddresses(addresses []domain.Address, record *domain.Record) error {
	byID := make(map[uuid.UUID]domain.Address, len(addresses))
	for _, address := range addresses {
		byID[address.ID] = address
	}
	prePay, ok := byID[record.PrePayAddress.ID]
	if !ok {
		return fmt.Errorf("pre-pay address does not belong to trip")
	}
	record.PrePayAddress = prePay
	seen := make(map[uuid.UUID]struct{}, len(record.ShouldPayAddress))
	for i := range record.ShouldPayAddress {
		address, ok := byID[record.ShouldPayAddress[i].Address.ID]
		if !ok {
			return fmt.Errorf("should-pay address at index %d does not belong to trip", i)
		}
		if _, duplicate := seen[address.ID]; duplicate {
			return fmt.Errorf("duplicate should-pay address %s", address.ID)
		}
		seen[address.ID] = struct{}{}
		record.ShouldPayAddress[i].Address = address
	}
	return nil
}

func paymentFromRecord(record domain.RecordInfo, addresses []domain.ExtendAddress) tx.UserPayment {
	payment := tx.UserPayment{Name: record.Name, Amount: record.Amount, PrePayAddress: record.PrePayAddress, ShouldPayAddress: make([]domain.Address, len(addresses)), ExtendPayMsg: make([]float64, len(addresses)), PaymentType: int(record.Category)}
	for i, address := range addresses {
		payment.ShouldPayAddress[i], payment.ExtendPayMsg[i] = address.Address, address.ExtendMsg
	}
	return payment
}

func clonePatch(p domain.RecordPatch) domain.RecordPatch {
	if p.Name != nil {
		v := *p.Name
		p.Name = &v
	}
	if p.Amount != nil {
		v := *p.Amount
		p.Amount = &v
	}
	if p.Time != nil {
		v := *p.Time
		p.Time = &v
	}
	if p.PrePayAddressID != nil {
		v := *p.PrePayAddressID
		p.PrePayAddressID = &v
	}
	if p.Category != nil {
		v := *p.Category
		p.Category = &v
	}
	if p.ShouldPayAddress != nil {
		v := cloneAddresses(*p.ShouldPayAddress)
		p.ShouldPayAddress = &v
	}
	if p.IsDeleted != nil {
		v := *p.IsDeleted
		p.IsDeleted = &v
	}
	return p
}
func cloneAddresses(v []domain.ExtendAddress) []domain.ExtendAddress { return slices.Clone(v) }
func cloneDomainRecord(v domain.Record) domain.Record {
	v.RecordInfo = cloneRecordInfo(v.RecordInfo)
	v.ShouldPayAddress = cloneAddresses(v.ShouldPayAddress)
	return v
}
func cloneRecordInfo(v domain.RecordInfo) domain.RecordInfo {
	if v.ParentRecordID != nil {
		parent := *v.ParentRecordID
		v.ParentRecordID = &parent
	}
	if v.ChildRecordID != nil {
		child := *v.ChildRecordID
		v.ChildRecordID = &child
	}
	return v
}
