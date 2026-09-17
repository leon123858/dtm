package pg

import (
	"context"
	"testing"

	"dtm/adapters/db/db"
	"dtm/adapters/db/internal/testutil"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestJoinedRecordsBindsUUIDs(t *testing.T) {
	for _, column := range []string{"trip_id", "id"} {
		t.Run(column, func(t *testing.T) {
			database, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=localhost user=postgres dbname=postgres", PreferSimpleProtocol: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			require.NoError(t, err)
			ids := []uuid.UUID{uuid.New(), uuid.New()}
			var sql string
			var vars []any
			require.NoError(t, database.Callback().Row().After("gorm:row").Register("capture", func(tx *gorm.DB) {
				sql = tx.Statement.SQL.String()
				vars = append([]any(nil), tx.Statement.Vars...)
			}))
			wrapper := &pgDBWrapper{db: database}
			// Scan reports ErrDryRunModeUnsupported after building the actual loader SQL.
			if column == "trip_id" {
				_, err = wrapper.DataLoaderGetTripRecords(context.Background(), ids, db.RecordReadOptions{HaveHistory: true})
			} else {
				_, err = wrapper.DataLoaderGetRecordList(context.Background(), ids)
			}
			require.ErrorIs(t, err, gorm.ErrDryRunModeUnsupported)
			require.Contains(t, sql, `"r"."`+column+`" IN ($1,$2)`)
			require.NotContains(t, sql, ids[0].String())
			require.Equal(t, []any{ids[0], ids[1]}, vars)
		})
	}
}

func TestSpecialNamesPostgresRoundTrip(t *testing.T) {
	wrapper, cleanup, tripID, payer, member := setupTrip(t)
	defer cleanup()
	ctx := context.Background()
	for _, name := range []string{"O'Brien", `'); DROP TABLE records; --`, ` <script>👩🏽‍💻 é `} {
		require.NoError(t, wrapper.UpdateTripInfo(&domain.TripInfo{ID: tripID, Name: name}))
		_, err := wrapper.UpdateAddress(tripID, payer.ID, name)
		require.NoError(t, err)
		record := pgPayment(uuid.New(), payer, member)
		record.Name = name
		_, err = wrapper.AppendNew(ctx, tripID, record, testutil.Materializer{})
		require.NoError(t, err)
		records, err := wrapper.DataLoaderGetRecordList(ctx, []uuid.UUID{record.ID})
		require.NoError(t, err)
		require.Equal(t, name, records[record.ID].Name)
		require.Equal(t, name, records[record.ID].PrePayAddress.Name)
		trips, err := wrapper.DataLoaderGetTripInfoList(ctx, []uuid.UUID{tripID})
		require.NoError(t, err)
		require.Equal(t, name, trips[tripID].Name)
		all, err := wrapper.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
		require.NoError(t, err)
		require.NotEmpty(t, all[tripID])
	}
}
