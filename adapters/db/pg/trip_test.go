package pg

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/internal/testutil"
	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
	_, first, appended, err := wrapper.AppendPatch(context.Background(), tripID, rootID, testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: name}), testutil.Materializer{})
	require.NoError(t, err)
	assert.True(t, appended)
	assert.Equal(t, rootID, *first.ParentRecordID)
	_, same, appended, err := wrapper.AppendPatch(context.Background(), tripID, rootID, testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: name}), testutil.Materializer{})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, first.ID, same.ID)

	var group sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		group.Add(1)
		go func(amount float64) {
			defer group.Done()
			_, _, _, appendErr := wrapper.AppendPatch(context.Background(), tripID, rootID, testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Amount: amount}), testutil.Materializer{})
			errs <- appendErr
		}(float64(i + 1))
	}
	group.Wait()
	close(errs)
	for appendErr := range errs {
		require.NoError(t, appendErr)
	}
	records, err := wrapper.DataLoaderGetTripRecords(context.Background(), []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	assert.Len(t, records[tripID], 8)
}

func TestChangelogAppendContract(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	testutil.CheckPatchContract(t, wrapper)
}

type sqlRecorder struct {
	logger.Interface
	statements []string
}

func (l *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	l.statements = append(l.statements, sql)
}

func TestAppendPatchQueryCountAndBatch(t *testing.T) {
	store, cleanup, tripID, payer, member := setupTrip(t)
	defer cleanup()
	wrapper := store.(*pgDBWrapper)
	ctx := context.Background()
	tail, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
	require.NoError(t, err)
	recorder := &sqlRecorder{Interface: wrapper.db.Logger}
	measured := &pgDBWrapper{db: wrapper.db.Session(&gorm.Session{Logger: recorder})}
	for i := 0; i < 5; i++ {
		recorder.statements = nil
		patch := testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: fmt.Sprintf("meal-%d", i), ShouldPayAddress: domain.RecordShares{{AddressID: payer.ID.String(), ExtendMsg: 1}, {AddressID: member.ID.String(), ExtendMsg: 2}}})
		_, next, appended, err := measured.AppendPatch(ctx, tripID, tail.ID, patch, testutil.Materializer{})
		require.NoError(t, err)
		require.True(t, appended)
		selects, batches := 0, 0
		for _, sql := range recorder.statements {
			if strings.HasPrefix(sql, "SELECT") {
				selects++
			}
			if strings.HasPrefix(sql, "INSERT INTO \"record_should_pay_address_lists\"") {
				batches++
			}
		}
		assert.Equal(t, 3, selects, recorder.statements)
		assert.Equal(t, 1, batches, recorder.statements)
		assert.Contains(t, recorder.statements[0], "FOR NO KEY UPDATE")
		got, err := store.DataLoaderGetRecordList(ctx, []uuid.UUID{next.ID})
		require.NoError(t, err)
		assert.ElementsMatch(t, next.ShouldPayAddress, got[next.ID].ShouldPayAddress)
		tail = next
	}
	_, _, _, err = store.AppendPatch(ctx, uuid.New(), tail.ID, domain.RecordPatch{}, testutil.Materializer{})
	require.ErrorIs(t, err, db.ErrRecordNotFound)
	_, _, _, err = store.AppendPatch(ctx, tripID, uuid.New(), domain.RecordPatch{}, testutil.Materializer{})
	require.ErrorIs(t, err, db.ErrRecordNotFound)
}

// Block inside the policy, after the writer has locked the tail.
type blockingPolicy struct {
	testutil.Materializer
	entered chan struct{}
	release chan struct{}
}

func (p blockingPolicy) ApplyPatch(tail domain.Record, patch domain.RecordPatch, addresses []domain.Address) (domain.Record, bool, error) {
	close(p.entered)
	<-p.release
	return p.Materializer.ApplyPatch(tail, patch, addresses)
}

func TestAppendPatchWaitsAndFollowsNewTail(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(fmt.Sprintf("historical=%v", historical), func(t *testing.T) {
			store, cleanup, tripID, payer, member := setupTrip(t)
			defer cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			root, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
			require.NoError(t, err)
			_, tail, _, err := store.AppendPatch(ctx, tripID, root.ID, testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: "middle"}), testutil.Materializer{})
			require.NoError(t, err)
			policy := blockingPolicy{entered: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(policy.release) }) }
			defer release()
			namePatch := testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: "concurrent"})
			amountPatch := testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Amount: 60})
			first := make(chan error, 1)
			go func() {
				_, _, _, err := store.AppendPatch(ctx, tripID, tail.ID, namePatch, policy)
				first <- err
			}()
			select {
			case <-policy.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Capture the waiting transaction PID before its first node query.
			pids := make(chan int, 1)
			secondDB := store.(*pgDBWrapper).db.Session(&gorm.Session{NewDB: true})
			require.NoError(t, secondDB.Callback().Query().Before("gorm:query").Register("test:waiting_pid", func(tx *gorm.DB) {
				if tx.Statement.Context.Value(waitingWriterKey{}) == true {
					var pid int
					if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err == nil {
						select {
						case pids <- pid:
						default:
						}
					}
				}
			}))
			defer secondDB.Callback().Query().Remove("test:waiting_pid")
			second := make(chan error, 1)
			target := tail.ID
			if historical {
				target = root.ID
			}
			go func() {
				_, _, _, err := (&pgDBWrapper{db: secondDB}).AppendPatch(context.WithValue(ctx, waitingWriterKey{}, true), tripID, target, amountPatch, testutil.Materializer{})
				second <- err
			}()
			var pid int
			select {
			case pid = <-pids:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Eventually(t, func() bool {
				var blocked bool
				err := store.(*pgDBWrapper).db.Raw("SELECT cardinality(pg_blocking_pids(?)) > 0", pid).Scan(&blocked).Error
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond)
			release()
			require.NoError(t, <-first)
			require.NoError(t, <-second)
			records, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
			require.NoError(t, err)
			require.Len(t, records[tripID], 4)
			tails := 0
			for _, record := range records[tripID] {
				if record.ChildRecordID == nil {
					tails++
					assert.Equal(t, "concurrent", record.Name)
					assert.Equal(t, float64(60), record.Amount)
				}
			}
			assert.Equal(t, 1, tails)
		})
	}
}

type waitingWriterKey struct{}

func TestAppendPatchBatchFailureRollsBack(t *testing.T) {
	store, cleanup, tripID, payer, member := setupTrip(t)
	defer cleanup()
	ctx := context.Background()
	root, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
	require.NoError(t, err)
	// Duplicate share rows violate the primary key during the batch INSERT,
	// after the old tail link and the new record have already been written.
	patch := testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{ShouldPayAddress: domain.RecordShares{
		{AddressID: member.ID.String(), ExtendMsg: 1},
		{AddressID: member.ID.String(), ExtendMsg: 2},
	}})
	_, _, appended, err := store.AppendPatch(ctx, tripID, root.ID, patch, testutil.Materializer{})
	require.Error(t, err)
	assert.False(t, appended)
	records, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, records[tripID], 1)
	assert.Nil(t, records[tripID][0].ChildRecordID)
	var shares []RecordShouldPayAddressListModel
	require.NoError(t, store.(*pgDBWrapper).db.Where("trip_id = ?", tripID).Find(&shares).Error)
	require.Len(t, shares, 1)
	assert.Equal(t, root.ID, shares[0].RecordID)
}

func TestAppendPatchForwardChainErrors(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		t.Run(fmt.Sprintf("cycle=%v", cycle), func(t *testing.T) {
			store, cleanup, tripID, payer, member := setupTrip(t)
			defer cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			root, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
			require.NoError(t, err)
			_, tail, _, err := store.AppendPatch(ctx, tripID, root.ID, testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: "tail"}), testutil.Materializer{})
			require.NoError(t, err)
			conn := store.(*pgDBWrapper).db
			want := chainlist.ErrInconsistentLink
			if cycle {
				require.NoError(t, conn.Transaction(func(tx *gorm.DB) error {
					if err := tx.Model(&RecordModel{}).Where("id = ?", root.ID).Update("parent_record_id", tail.ID).Error; err != nil {
						return err
					}
					return tx.Model(&RecordModel{}).Where("id = ?", tail.ID).Update("child_record_id", root.ID).Error
				}))
				want = chainlist.ErrCycle
			} else {
				require.NoError(t, conn.Model(&RecordModel{}).Where("id = ?", tail.ID).Update("parent_record_id", nil).Error)
			}
			_, _, _, err = store.AppendPatch(ctx, tripID, root.ID, domain.RecordPatch{}, testutil.Materializer{})
			require.ErrorIs(t, err, db.ErrInvalidChain)
			require.ErrorIs(t, err, want)
		})
	}
}

func TestAppendPatchDeleteAndRestore(t *testing.T) {
	store, cleanup, tripID, payer, member := setupTrip(t)
	defer cleanup()
	ctx := context.Background()
	root, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
	require.NoError(t, err)
	deletion := testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{IsDeleted: true})
	_, deleted, appended, err := store.AppendPatch(ctx, tripID, root.ID, deletion, testutil.Materializer{})
	require.NoError(t, err)
	require.True(t, appended)
	require.True(t, deleted.IsDeleted)
	_, same, appended, err := store.AppendPatch(ctx, tripID, root.ID, deletion, testutil.Materializer{})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, deleted.ID, same.ID)
	_, restored, appended, err := store.AppendPatch(ctx, tripID, deleted.ID, testutil.Patch(t, domain.RecordFields{IsDeleted: true}, domain.RecordFields{}), testutil.Materializer{})
	require.NoError(t, err)
	assert.True(t, appended)
	assert.False(t, restored.IsDeleted)
	assert.Equal(t, deleted.ID, *restored.ParentRecordID)
}

func TestCompleteRecordReadContract(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	testutil.CheckReadContract(t, store)
}

func TestCompleteRecordReadsUseOneSelect(t *testing.T) {
	store, cleanup, tripID, payer, member := setupTrip(t)
	defer cleanup()
	ctx := context.Background()
	var ids []uuid.UUID
	for range 3 {
		record, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
		require.NoError(t, err)
		ids = append(ids, record.ID)
	}
	recorder := &sqlRecorder{Interface: logger.Default}
	wrapper := &pgDBWrapper{db: store.(*pgDBWrapper).db.Session(&gorm.Session{Logger: recorder})}
	for _, history := range []bool{false, true} {
		recorder.statements = nil
		got, err := wrapper.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: history})
		require.NoError(t, err)
		require.Len(t, got[tripID], 3)
		require.Len(t, recorder.statements, 1)
		assert.Contains(t, recorder.statements[0], "LEFT JOIN")
		assert.Equal(t, !history, strings.Contains(recorder.statements[0], "child_record_id IS NULL"))
	}
	recorder.statements = nil
	_, err := wrapper.DataLoaderGetRecordList(ctx, ids)
	require.NoError(t, err)
	require.Len(t, recorder.statements, 1)
	recorder.statements = nil
	_, err = wrapper.DataLoaderGetTripRecords(ctx, nil, db.RecordReadOptions{})
	require.NoError(t, err)
	_, err = wrapper.DataLoaderGetRecordList(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, recorder.statements)
}
