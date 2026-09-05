package pg

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/internal/testutil"
	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestDSN() string {
	if value := os.Getenv("TEST_DATABASE_URL"); value != "" {
		return value
	}
	return CreateDSN()
}

func setupTestDB(t *testing.T) (db.TripDBWrapper, func()) {
	t.Helper()
	gormDB, err := InitPostgresGORM(getTestDSN())
	if err != nil {
		t.Skipf("PostgreSQL is unavailable: %v", err)
	}
	wrapper := NewPgDBWrapper(gormDB)
	cleanup := func() {
		require.NoError(t, gormDB.Exec("TRUNCATE TABLE record_should_pay_address_lists, records, addresses, trips RESTART IDENTITY CASCADE").Error)
		if sqlDB, sqlErr := gormDB.DB(); sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	}
	return wrapper, cleanup
}

func setupTrip(t *testing.T) (db.TripDBWrapper, func(), uuid.UUID, domain.Address, domain.Address) {
	t.Helper()
	wrapper, cleanup := setupTestDB(t)
	tripID := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
	payer, err := wrapper.CreateAddress(tripID, "payer")
	require.NoError(t, err)
	member, err := wrapper.CreateAddress(tripID, "member")
	require.NoError(t, err)
	return wrapper, cleanup, tripID, *payer, *member
}

func pgPayment(id uuid.UUID, payer, member domain.Address) domain.Record {
	return domain.Record{RecordInfo: domain.RecordInfo{ID: id, Name: "meal", Amount: 20, Time: time.Now(), PrePayAddress: payer, Category: domain.CategoryNormal}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: member}}}}
}

func TestRecordInfoFromModelIncludesLinksAndDeletion(t *testing.T) {
	parent, child, prepay := uuid.New(), uuid.New(), uuid.New()
	info := recordInfoFromModel(RecordModel{ID: uuid.New(), ParentRecordID: &parent, ChildRecordID: &child, Name: "x", Amount: 2, PrePayAddressID: prepay, IsDeleted: true}, map[uuid.UUID]domain.Address{prepay: {ID: prepay}})
	assert.Equal(t, parent, *info.ParentRecordID)
	assert.Equal(t, child, *info.ChildRecordID)
	assert.True(t, info.IsDeleted)
}

func TestAppendPatchRoundTripAndConcurrentSerialization(t *testing.T) {
	wrapper, cleanup, tripID, payer, member := setupTrip(t)
	defer cleanup()
	rootID := uuid.New()
	_, err := wrapper.AppendNew(context.Background(), tripID, pgPayment(rootID, payer, member), testutil.Materializer{})
	require.NoError(t, err)
	name := "dinner"
	_, first, appended, err := wrapper.AppendPatch(context.Background(), rootID, domain.RecordPatch{Name: &name}, testutil.Materializer{})
	require.NoError(t, err)
	assert.True(t, appended)
	assert.Equal(t, rootID, *first.ParentRecordID)
	_, same, appended, err := wrapper.AppendPatch(context.Background(), rootID, domain.RecordPatch{Name: &name}, testutil.Materializer{})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, first.ID, same.ID)

	var group sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		group.Add(1)
		go func(amount float64) {
			defer group.Done()
			_, _, _, appendErr := wrapper.AppendPatch(context.Background(), rootID, domain.RecordPatch{Amount: &amount}, testutil.Materializer{})
			errs <- appendErr
		}(float64(i + 1))
	}
	group.Wait()
	close(errs)
	for appendErr := range errs {
		require.NoError(t, appendErr)
	}
	records, err := wrapper.DataLoaderGetRecordInfoList(context.Background(), []uuid.UUID{tripID})
	require.NoError(t, err)
	assert.Len(t, records[tripID], 8)
}
