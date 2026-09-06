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
