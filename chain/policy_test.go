package chain

import (
	"errors"
	"testing"

	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordPolicyPrepareNewCanonicalizesAndValidates(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	value := testPayment(uuid.New(), domain.Address{ID: payer.ID, Name: "stale"}, domain.Address{ID: member.ID, Name: "stale"})

	got, err := (recordPolicy{}).PrepareNew(value, []domain.Address{payer, member})
	require.NoError(t, err)
	assert.Equal(t, payer, got.PrePayAddress)
	assert.Equal(t, member, got.ShouldPayAddress[0].Address)
	assert.Equal(t, "stale", value.PrePayAddress.Name, "policy must not mutate caller-owned data")
}

func TestRecordPolicyReturnsStableValidationErrors(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	valid := testPayment(uuid.New(), payer, member)

	tests := []struct {
		name   string
		change func(*domain.Record)
		want   error
	}{
		{name: "missing ID", change: func(v *domain.Record) { v.ID = uuid.Nil }, want: ErrInvalidRecordSnapshot},
		{name: "unsafe name", change: func(v *domain.Record) { v.Name = "<meal>" }, want: ErrInvalidRecordSnapshot},
		{name: "non-positive amount", change: func(v *domain.Record) { v.Amount = 0 }, want: ErrInvalidRecordSnapshot},
		{name: "unknown category", change: func(v *domain.Record) { v.Category = domain.RecordCategory(99) }, want: ErrInvalidRecordSnapshot},
		{name: "no recipients", change: func(v *domain.Record) { v.ShouldPayAddress = nil }, want: ErrInvalidRecordSnapshot},
		{name: "duplicate recipients", change: func(v *domain.Record) { v.ShouldPayAddress = append(v.ShouldPayAddress, v.ShouldPayAddress[0]) }, want: ErrInvalidRecordAddresses},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := cloneDomainRecord(valid)
			tt.change(&value)
			err := (recordPolicy{}).Validate(value)
			require.ErrorIs(t, err, tt.want)
			assert.True(t, errors.Is(err, ErrInvalidRecordSnapshot))
		})
	}
}

func TestRecordPolicyRejectsDeletedCreateAndForeignAddress(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	value := testPayment(uuid.New(), payer, member)
	value.IsDeleted = true

	_, err := (recordPolicy{}).PrepareNew(value, []domain.Address{payer, member})
	require.ErrorIs(t, err, ErrInvalidRecordSnapshot)

	value.IsDeleted = false
	_, err = (recordPolicy{}).PrepareNew(value, []domain.Address{payer})
	require.ErrorIs(t, err, ErrInvalidRecordAddresses)
	require.ErrorIs(t, err, ErrInvalidRecordSnapshot)
}
