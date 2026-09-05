package chain

import (
	"fmt"
	"reflect"
	"slices"
	"unicode"

	"dtm/db/db"
	"dtm/domain"
	"dtm/tx"

	"github.com/google/uuid"
)

type recordPolicy struct{}

var _ db.RecordMaterializer = recordPolicy{}

func (recordPolicy) PrepareNew(value domain.Record, addresses []domain.Address) (domain.Record, error) {
	value = cloneDomainRecord(value)
	if value.ParentRecordID != nil || value.ChildRecordID != nil {
		return domain.Record{}, invalidSnapshot("new record must not contain chain links")
	}
	if value.IsDeleted {
		return domain.Record{}, invalidSnapshot("new record cannot be deleted")
	}
	if err := canonicalizeRecordAddresses(addresses, &value); err != nil {
		return domain.Record{}, err
	}
	if err := (recordPolicy{}).Validate(value); err != nil {
		return domain.Record{}, err
	}
	return value, nil
}

func (recordPolicy) ApplyPatch(tail domain.Record, patch domain.RecordPatch, addresses []domain.Address) (domain.Record, bool, error) {
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
	if err := canonicalizeRecordAddresses(addresses, &result); err != nil {
		return domain.Record{}, false, err
	}
	if err := (recordPolicy{}).Validate(result); err != nil {
		return domain.Record{}, false, err
	}
	return result, !reflect.DeepEqual(result, tail), nil
}

func (recordPolicy) Validate(value domain.Record) error {
	if value.ID == uuid.Nil {
		return invalidSnapshot("record ID is required")
	}
	if !validRecordName(value.Name) {
		return invalidSnapshot("record name is invalid")
	}
	if value.Amount <= 0 {
		return invalidSnapshot("record amount must be positive")
	}
	if value.Category < domain.CategoryNormal || value.Category > domain.CategoryTransfer {
		return invalidSnapshot("record category is invalid")
	}
	if value.PrePayAddress.ID == uuid.Nil {
		return invalidSnapshot("pre-pay address is required")
	}
	if len(value.ShouldPayAddress) == 0 || len(value.ShouldPayAddress) > 100 {
		return invalidSnapshot("record must contain between 1 and 100 should-pay addresses")
	}
	seen := make(map[uuid.UUID]struct{}, len(value.ShouldPayAddress))
	for i, address := range value.ShouldPayAddress {
		if address.Address.ID == uuid.Nil {
			return invalidSnapshot("should-pay address at index %d is required", i)
		}
		if _, duplicate := seen[address.Address.ID]; duplicate {
			return fmt.Errorf("%w: %w: duplicate should-pay address %s", ErrInvalidRecordSnapshot, ErrInvalidRecordAddresses, address.Address.ID)
		}
		seen[address.Address.ID] = struct{}{}
	}
	payment := paymentFromRecord(value.RecordInfo, value.ShouldPayAddress)
	transaction, err := payment.ToTx(tx.ShareMoneyStrategyFactory(payment.PaymentType))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRecordSnapshot, err)
	}
	if !transaction.BoolValidate() {
		return invalidSnapshot("record payment does not balance")
	}
	return nil
}

func invalidSnapshot(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRecordSnapshot, fmt.Sprintf(format, args...))
}

func validRecordName(value string) bool {
	if len(value) == 0 || len(value) > 100 {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '_', '-', '.', '@', '#', ' ':
		default:
			return false
		}
	}
	return true
}

func canonicalizeRecordAddresses(addresses []domain.Address, value *domain.Record) error {
	byID := make(map[uuid.UUID]domain.Address, len(addresses))
	for _, address := range addresses {
		byID[address.ID] = address
	}
	prePay, ok := byID[value.PrePayAddress.ID]
	if !ok {
		return fmt.Errorf("%w: %w: pre-pay address does not belong to trip", ErrInvalidRecordSnapshot, ErrInvalidRecordAddresses)
	}
	value.PrePayAddress = prePay
	seen := make(map[uuid.UUID]struct{}, len(value.ShouldPayAddress))
	for i := range value.ShouldPayAddress {
		address, ok := byID[value.ShouldPayAddress[i].Address.ID]
		if !ok {
			return fmt.Errorf("%w: %w: should-pay address at index %d does not belong to trip", ErrInvalidRecordSnapshot, ErrInvalidRecordAddresses, i)
		}
		if _, duplicate := seen[address.ID]; duplicate {
			return fmt.Errorf("%w: %w: duplicate should-pay address %s", ErrInvalidRecordSnapshot, ErrInvalidRecordAddresses, address.ID)
		}
		seen[address.ID] = struct{}{}
		value.ShouldPayAddress[i].Address = address
	}
	return nil
}

func paymentFromRecord(value domain.RecordInfo, addresses []domain.ExtendAddress) tx.UserPayment {
	payment := tx.UserPayment{Name: value.Name, Amount: value.Amount, PrePayAddress: value.PrePayAddress, ShouldPayAddress: make([]domain.Address, len(addresses)), ExtendPayMsg: make([]float64, len(addresses)), PaymentType: int(value.Category)}
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
