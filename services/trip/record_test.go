package trip

import (
	"testing"

	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordValidateUsesPayload(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*domain.Record)
		valid bool
	}{
		{"valid", func(*domain.Record) {}, true},
		{"empty ID", func(r *domain.Record) { r.ID = uuid.Nil }, false},
		{"unknown category", func(r *domain.Record) { r.Category = 99 }, false},
		{"invalid amount", func(r *domain.Record) { r.Amount = 0 }, false},
		{"missing shares", func(r *domain.Record) { r.ShouldPayAddress = nil }, false},
		{"duplicate shares", func(r *domain.Record) { r.ShouldPayAddress = append(r.ShouldPayAddress, r.ShouldPayAddress[0]) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := testPayment(uuid.New(), domain.Address{ID: uuid.New()}, domain.Address{ID: uuid.New()})
			tc.edit(&value)
			factory := NewRecordFactory(nil)
			valid, err := factory.FromRecord(value).Validate()
			require.NoError(t, err)
			assert.Equal(t, tc.valid, valid)
		})
	}
}

func TestDomainRecordSortsDetachedShares(t *testing.T) {
	a := domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Name: "A"}
	b := domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Name: "B"}
	value := testPayment(uuid.New(), a, b)
	value.ShouldPayAddress = []domain.ExtendAddress{{Address: b, ExtendMsg: 20}, {Address: a, ExtendMsg: 10}}
	loaded := NewRecordFactory(nil).FromRecord(value)
	got := loaded.DomainRecord()
	assert.Equal(t, []domain.ExtendAddress{{Address: a, ExtendMsg: 10}, {Address: b, ExtendMsg: 20}}, got.ShouldPayAddress)
	assert.Equal(t, value.ShouldPayAddress, loaded.GetShouldPay(), "sorting the output must not reorder the internal record")
	got.ShouldPayAddress[0].Address.Name = "caller mutation"
	got.ShouldPayAddress[0].ExtendMsg = 99
	assert.Equal(t, value.ShouldPayAddress, loaded.GetShouldPay(), "output must remain detached")
	assert.Equal(t, b, value.ShouldPayAddress[0].Address)
}
