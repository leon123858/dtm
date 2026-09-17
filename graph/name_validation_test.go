package graph

import (
	"context"
	"strings"
	"testing"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/domain"
	"dtm/graph/model"
	"dtm/libs/textvalidate"
	tripservice "dtm/services/trip"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNameValidationOnEveryMutation(t *testing.T) {
	database := mem.NewInMemoryTripDBWrapper()
	tripID := uuid.New()
	require.NoError(t, database.CreateTrip(&domain.TripInfo{ID: tripID, Name: "旅行 ✈️"}))
	payer, err := database.CreateAddress(tripID, "成員 👩🏽‍💻")
	require.NoError(t, err)
	recordID := uuid.New()
	_, err = database.AppendNew(context.Background(), tripID, domain.Record{
		RecordInfo: domain.RecordInfo{ID: recordID, Name: "<meal> 🍱", Amount: 20, PrePayAddress: *payer},
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *payer}}},
	}, seedRecordPolicy{})
	require.NoError(t, err)
	resolver := &mutationResolver{Resolver: resolverWithChain(database)}
	old := model.NewRecord{Name: "<meal> 🍱", Amount: 20, PrePayAddressID: payer.ID.String(), ShouldPayAddressIds: []string{payer.ID.String()}}
	for _, tc := range []struct {
		name, value, reason string
		kind                error
	}{
		{"invalid UTF-8", "\xff", "contains invalid characters: invalid UTF-8", textvalidate.ErrInvalidUTF8},
		{"control", "\n", "contains invalid characters: U+000A is not allowed", textvalidate.ErrInvalidCharacter},
		{"line separator", "\u2028", "contains invalid characters: U+2028 is not allowed", textvalidate.ErrInvalidCharacter},
		{"paragraph separator", "\u2029", "contains invalid characters: U+2029 is not allowed", textvalidate.ErrInvalidCharacter},
		{"empty", "", "must not be empty or whitespace-only", textvalidate.ErrEmptyName},
		{"whitespace", " \u3000", "must not be empty or whitespace-only", textvalidate.ErrEmptyName},
		{"too long", strings.Repeat("🍜", 101), "must not exceed 100 Unicode code points", textvalidate.ErrNameTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := old
			next.Name = tc.value
			ctx := resolverContext(database)
			operations := []struct {
				name, field string
				run         func() error
			}{
				{"create trip", "trip", func() error { _, err := resolver.CreateTrip(ctx, model.NewTrip{Name: tc.value}); return err }},
				{"update trip", "trip", func() error {
					_, err := resolver.UpdateTrip(ctx, tripID.String(), model.NewTrip{Name: tc.value})
					return err
				}},
				{"create address", "address", func() error {
					_, err := resolver.CreateAddress(ctx, tripID.String(), model.NewAddress{Name: tc.value})
					return err
				}},
				{"update address", "address", func() error {
					_, err := resolver.UpdateAddress(ctx, tripID.String(), payer.ID.String(), model.NewAddress{Name: tc.value})
					return err
				}},
				{"create record", "record", func() error { _, err := resolver.CreateRecord(ctx, tripID.String(), next); return err }},
				{"update record", "record", func() error {
					_, err := resolver.UpdateRecord(ctx, recordID.String(), model.EditRecord{Old: &old, New: &next})
					return err
				}},
			}
			for _, operation := range operations {
				t.Run(operation.name, func(t *testing.T) {
					err := operation.run()
					require.ErrorIs(t, err, tc.kind)
					require.True(t, strings.HasSuffix(err.Error(), operation.field+" name "+tc.reason), err.Error())
					if operation.field == "record" {
						require.ErrorIs(t, err, tripservice.ErrInvalidRecordSnapshot)
					}
				})
			}
		})
	}
	records, err := database.DataLoaderGetTripRecords(context.Background(), []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, records[tripID], 1)
	require.Equal(t, "<meal> 🍱", records[tripID][0].Name)
}
