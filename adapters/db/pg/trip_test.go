package pg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/testutil"
	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
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
	wrapper := NewPgTripDBWrapper(gormDB)
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
	payer, err := wrapper.CreateAddress(context.Background(), tripID, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("payer")}})
	require.NoError(t, err)
	member, err := wrapper.CreateAddress(context.Background(), tripID, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("member")}})
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
	wrapper := store.(*pgTripDBWrapper)
	ctx := context.Background()
	tail, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
	require.NoError(t, err)
	recorder := &sqlRecorder{Interface: wrapper.db.Logger}
	measured := &pgTripDBWrapper{db: wrapper.db.Session(&gorm.Session{Logger: recorder})}
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
		assert.Equal(t, 4, selects, recorder.statements)
		assert.Equal(t, 1, batches, recorder.statements)
		assert.Contains(t, recorder.statements[0], "FOR SHARE")
		assert.Contains(t, recorder.statements[1], "FOR NO KEY UPDATE")
		got, err := store.DataLoaderGetRecordList(ctx, []uuid.UUID{next.ID})
		require.NoError(t, err)
		assert.ElementsMatch(t, next.ShouldPayAddress, got[next.ID].ShouldPayAddress)
		tail = next
	}
	_, _, _, err = store.AppendPatch(ctx, uuid.New(), tail.ID, domain.RecordPatch{}, testutil.Materializer{})
	require.ErrorIs(t, err, domain.ErrTripNotFound)
	_, _, _, err = store.AppendPatch(ctx, tripID, uuid.New(), domain.RecordPatch{}, testutil.Materializer{})
	require.ErrorIs(t, err, domain.ErrRecordNotFound)
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
			secondDB := store.(*pgTripDBWrapper).db.Session(&gorm.Session{NewDB: true})
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
				_, _, _, err := (&pgTripDBWrapper{db: secondDB}).AppendPatch(context.WithValue(ctx, waitingWriterKey{}, true), tripID, target, amountPatch, testutil.Materializer{})
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
				err := store.(*pgTripDBWrapper).db.Raw("SELECT cardinality(pg_blocking_pids(?)) > 0", pid).Scan(&blocked).Error
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

// Inject invalid materialized rows to exercise database rollback independently
// of the identifier-based differ, which cannot emit duplicate members.
type duplicateSharePolicy struct{ testutil.Materializer }

func (duplicateSharePolicy) ApplyPatch(tail domain.Record, _ domain.RecordPatch, _ []domain.Address) (domain.Record, bool, error) {
	tail.ShouldPayAddress = []domain.ExtendAddress{tail.ShouldPayAddress[0], tail.ShouldPayAddress[0]}
	return tail, true, nil
}

func TestAppendPatchBatchFailureRollsBack(t *testing.T) {
	store, cleanup, tripID, payer, member := setupTrip(t)
	defer cleanup()
	ctx := context.Background()
	root, err := store.AppendNew(ctx, tripID, pgPayment(uuid.New(), payer, member), testutil.Materializer{})
	require.NoError(t, err)
	// Duplicate share rows violate the primary key during the batch INSERT,
	// after the old tail link and the new record have already been written.
	patch := domain.RecordPatch{}
	_, _, appended, err := store.AppendPatch(ctx, tripID, root.ID, patch, duplicateSharePolicy{})
	require.Error(t, err)
	assert.False(t, appended)
	records, err := store.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, err)
	require.Len(t, records[tripID], 1)
	assert.Nil(t, records[tripID][0].ChildRecordID)
	var shares []RecordShouldPayAddressListModel
	require.NoError(t, store.(*pgTripDBWrapper).db.Where("trip_id = ?", tripID).Find(&shares).Error)
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
			conn := store.(*pgTripDBWrapper).db
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
			require.ErrorIs(t, err, domain.ErrInvalidChain)
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

func TestCompleteRecordReadsUseBatchedAuthorization(t *testing.T) {
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
	wrapper := &pgTripDBWrapper{db: store.(*pgTripDBWrapper).db.Session(&gorm.Session{Logger: recorder})}
	for _, history := range []bool{false, true} {
		recorder.statements = nil
		got, err := wrapper.DataLoaderGetTripRecords(ctx, []uuid.UUID{tripID}, db.RecordReadOptions{HaveHistory: history})
		require.NoError(t, err)
		require.Len(t, got[tripID], 3)
		require.Len(t, recorder.statements, 2)
		assert.Contains(t, recorder.statements[1], "LEFT JOIN")
		assert.Equal(t, !history, strings.Contains(recorder.statements[1], "child_record_id IS NULL"))
	}
	recorder.statements = nil
	_, err := wrapper.DataLoaderGetRecordList(ctx, ids)
	require.NoError(t, err)
	require.Len(t, recorder.statements, 2)
	recorder.statements = nil
	_, err = wrapper.DataLoaderGetTripRecords(ctx, nil, db.RecordReadOptions{})
	require.NoError(t, err)
	_, err = wrapper.DataLoaderGetRecordList(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, recorder.statements)
}

func TestSharePatchContract(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	testutil.CheckSharePatchContract(t, wrapper)
}

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
			wrapper := &pgTripDBWrapper{db: database}
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
		require.NoError(t, wrapper.UpdateTripInfo(context.Background(), &domain.TripInfo{ID: tripID, Name: name}))
		_, _, err := wrapper.UpdateAddress(context.Background(), tripID, payer.ID, domain.AddressOption{Name: addressNamePtr(name)})
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

func lockTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func lockTestMember(t *testing.T, p *pgTripDBWrapper, ctx context.Context, id uuid.UUID) context.Context {
	t.Helper()
	u, err := NewPgAuthDBWrapper(p.db).UpsertUser(ctx, uuid.NewString())
	require.NoError(t, err)
	ctx = domain.WithUserID(ctx, u.ID)
	require.NoError(t, p.Bind(ctx, id, nil))
	return ctx
}

// Observe a real PostgreSQL lock wait; timing alone cannot prove exclusion.
func observeWaiter(t *testing.T, p *pgTripDBWrapper, ctx context.Context) (context.Context, func()) {
	t.Helper()
	pids := make(chan int, 1)
	name := "test:lock_wait"
	require.NoError(t, p.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(waitingWriterKey{}) == true {
			var pid int
			if tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error == nil {
				select {
				case pids <- pid:
				default:
				}
			}
		}
	}))
	t.Cleanup(func() { p.db.Callback().Query().Remove(name) })
	return context.WithValue(ctx, waitingWriterKey{}, true), func() {
		var pid int
		select {
		case pid = <-pids:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		require.Eventually(t, func() bool {
			var blocked bool
			return p.db.Raw("SELECT cardinality(pg_blocking_pids(?)) > 0", pid).Scan(&blocked).Error == nil && blocked
		}, 3*time.Second, 5*time.Millisecond)
	}
}

func TestIndependentChainsEnterTogether(t *testing.T) {
	s, cleanup, id, a, b := setupTrip(t)
	defer cleanup()
	ctx := lockTestContext(t)
	roots := []uuid.UUID{uuid.New(), uuid.New()}
	for _, root := range roots {
		_, err := s.AppendNew(ctx, id, pgPayment(root, a, b), testutil.Materializer{})
		require.NoError(t, err)
	}
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	done := make(chan error, 2)
	entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
	patch := testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: "parallel"})
	for i := range roots {
		go func(i int) {
			_, _, _, err := s.AppendPatch(ctx, id, roots[i], patch, blockingPolicy{entered: entered[i], release: release})
			done <- err
		}(i)
	}
	for _, ch := range entered {
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal("independent chain was serialized", ctx.Err())
		}
	}
	finish()
	require.NoError(t, <-done)
	require.NoError(t, <-done)
}

func TestExclusiveChangesWaitForWriter(t *testing.T) {
	for _, operation := range []string{"private", "unbind"} {
		t.Run(operation, func(t *testing.T) {
			s, cleanup, id, a, b := setupTrip(t)
			defer cleanup()
			p := s.(*pgTripDBWrapper)
			ctx := lockTestContext(t)
			owner := lockTestMember(t, p, ctx, id)
			root, err := s.AppendNew(ctx, id, pgPayment(uuid.New(), a, b), testutil.Materializer{})
			require.NoError(t, err)
			policy := blockingPolicy{entered: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(policy.release) }) }
			defer release()
			writer := make(chan error, 1)
			patch := testutil.Patch(t, domain.RecordFields{}, domain.RecordFields{Name: "held"})
			go func() { _, _, _, err := s.AppendPatch(ctx, id, root.ID, patch, policy); writer <- err }()
			select {
			case <-policy.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			waiting, blocked := observeWaiter(t, p, owner)
			done := make(chan error, 1)
			go func() {
				if operation == "private" {
					_, err := s.SetTripPrivate(waiting, id, true)
					done <- err
				} else {
					done <- s.Unbind(waiting, id)
				}
			}()
			blocked()
			release()
			require.NoError(t, <-writer)
			require.NoError(t, <-done)
		})
	}
}

func TestWaitingWriterRechecksRemovedMembership(t *testing.T) {
	s, cleanup, id, _, _ := setupTrip(t)
	defer cleanup()
	p := s.(*pgTripDBWrapper)
	ctx := lockTestContext(t)
	owner := lockTestMember(t, p, ctx, id)
	member := lockTestMember(t, p, ctx, id)
	_, err := s.SetTripPrivate(owner, id, true)
	require.NoError(t, err)
	tx := p.db.WithContext(ctx).Begin()
	defer tx.Rollback()
	_, err = lockTrip(tx, id, tripExclusive)
	require.NoError(t, err)
	uid, _ := domain.UserIDFromContext(member)
	require.NoError(t, tx.Where("trip_id = ? AND user_id = ?", id, uid).Delete(&TripBindingModel{}).Error)
	waiting, blocked := observeWaiter(t, p, member)
	done := make(chan error, 1)
	go func() {
		_, err := s.CreateAddress(waiting, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("denied")}})
		done <- err
	}()
	blocked()
	require.NoError(t, tx.Commit().Error)
	require.ErrorIs(t, <-done, domain.ErrForbidden)
}

func TestMembershipRacesKeepPrivateTripOccupied(t *testing.T) {
	for _, operation := range []string{"two-unbinds", "private-versus-unbind"} {
		t.Run(operation, func(t *testing.T) {
			s, cleanup, id, _, _ := setupTrip(t)
			defer cleanup()
			p := s.(*pgTripDBWrapper)
			ctx := lockTestContext(t)
			owner := lockTestMember(t, p, ctx, id)
			other := owner
			if operation == "two-unbinds" {
				other = lockTestMember(t, p, ctx, id)
				_, err := s.SetTripPrivate(owner, id, true)
				require.NoError(t, err)
			}
			start := make(chan struct{})
			done := make(chan error, 2)
			go func() { <-start; done <- s.Unbind(owner, id) }()
			go func() {
				<-start
				if operation == "two-unbinds" {
					done <- s.Unbind(other, id)
				} else {
					_, err := s.SetTripPrivate(other, id, true)
					done <- err
				}
			}()
			close(start)
			successes := 0
			for i := 0; i < 2; i++ {
				err := <-done
				if err == nil {
					successes++
				} else {
					require.True(t, errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrForbidden), err)
				}
			}
			require.Equal(t, 1, successes)
			var invalid int64
			require.NoError(t, p.db.Table("trips").Where("id = ? AND private AND NOT EXISTS (SELECT 1 FROM trip_bindings WHERE trip_id = trips.id)", id).Count(&invalid).Error)
			require.Zero(t, invalid)
		})
	}
}

func TestAddressClaimConflictRollsBackOriginalBinding(t *testing.T) {
	s, cleanup, id, a, b := setupTrip(t)
	defer cleanup()
	p := s.(*pgTripDBWrapper)
	ctx := lockTestContext(t)
	owners := []context.Context{lockTestMember(t, p, ctx, id), lockTestMember(t, p, ctx, id)}
	old := []uuid.UUID{a.ID, b.ID}
	for i := range owners {
		require.NoError(t, s.Bind(owners[i], id, &old[i]))
	}
	target, err := s.CreateAddress(ctx, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("target")}})
	require.NoError(t, err)
	// Both transactions finish the occupancy precheck before either upsert.
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	require.NoError(t, p.db.Callback().Create().Before("gorm:create").Register("test:claim", func(tx *gorm.DB) {
		if tx.Statement.Table == "trip_bindings" {
			arrived <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}))
	defer p.db.Callback().Create().Remove("test:claim")
	results := make(chan struct {
		i   int
		err error
	}, 2)
	for i := range owners {
		go func(i int) {
			results <- struct {
				i   int
				err error
			}{i, s.Bind(owners[i], id, &target.ID)}
		}(i)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	finish()
	success := 0
	for i := 0; i < 2; i++ {
		r := <-results
		binding, err := s.Binding(owners[r.i], id)
		require.NoError(t, err)
		require.NotNil(t, binding.Address)
		if r.err == nil {
			success++
			require.Equal(t, target.ID, binding.Address.ID)
		} else {
			require.ErrorIs(t, r.err, domain.ErrConflict)
			require.Equal(t, old[r.i], binding.Address.ID)
		}
	}
	require.Equal(t, 1, success)
}

func TestBindDeleteAddressLockOrder(t *testing.T) {
	for _, first := range []string{"bind", "delete"} {
		t.Run(first, func(t *testing.T) {
			s, cleanup, id, a, _ := setupTrip(t)
			defer cleanup()
			p := s.(*pgTripDBWrapper)
			ctx := lockTestContext(t)
			owner := lockTestMember(t, p, ctx, id)
			tx := p.db.WithContext(ctx).Begin()
			defer tx.Rollback()
			require.NoError(t, lockAccess(tx, owner, id, tripShared))
			strength := "UPDATE"
			if first == "bind" {
				strength = "KEY SHARE"
			}
			require.NoError(t, tx.Exec("SELECT id FROM addresses WHERE id = ? FOR "+strength, a.ID).Error)
			waiting, blocked := observeWaiter(t, p, owner)
			done := make(chan error, 1)
			go func() {
				if first == "bind" {
					_, err := s.DeleteAddress(waiting, id, a.ID)
					done <- err
				} else {
					done <- s.Bind(waiting, id, &a.ID)
				}
			}()
			blocked()
			if first == "bind" {
				uid, _ := domain.UserIDFromContext(owner)
				require.NoError(t, tx.Model(&TripBindingModel{}).Where("trip_id = ? AND user_id = ?", id, uid).Update("address_id", a.ID).Error)
			} else {
				require.NoError(t, tx.Delete(&AddressModel{}, "id = ?", a.ID).Error)
			}
			require.NoError(t, tx.Commit().Error)
			expected := domain.ErrConflict
			if first == "delete" {
				expected = domain.ErrBadUserInput
			}
			require.ErrorIs(t, <-done, expected)
		})
	}
}

func TestCanceledLockWaitAndRollbackReleaseLocks(t *testing.T) {
	s, cleanup, id, _, _ := setupTrip(t)
	defer cleanup()
	p := s.(*pgTripDBWrapper)
	ctx := lockTestContext(t)
	tx := p.db.WithContext(ctx).Begin()
	defer tx.Rollback()
	_, err := lockTrip(tx, id, tripExclusive)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	waiting, blocked := observeWaiter(t, p, canceled)
	done := make(chan error, 1)
	go func() {
		_, err := s.CreateAddress(waiting, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("canceled")}})
		done <- err
	}()
	blocked()
	cancel()
	require.Error(t, <-done)
	require.NoError(t, tx.Rollback().Error)
	_, err = s.CreateAddress(ctx, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("after rollback")}})
	require.NoError(t, err)
}

func TestConstraintErrorMapping(t *testing.T) {
	for _, driver := range []string{"pgx", "pq"} {
		for _, tc := range []struct {
			code, name        string
			binding, deletion error
		}{
			{"23505", "trip_bindings_address", domain.ErrConflict, nil},
			{"23503", "trip_bindings_trip_id_address_id_fkey", domain.ErrBadUserInput, domain.ErrConflict},
			{"23503", "trip_bindings_user_id_fkey", domain.ErrUserNotFound, nil},
			{"23503", "trip_bindings_trip_id_fkey", domain.ErrTripNotFound, nil},
			{"23503", "fk_records_trip_address", nil, domain.ErrConflict},
			{"23503", "fk_rspl_trip_address", nil, domain.ErrConflict},
			{"23505", "unrelated", nil, nil},
			{"23503", "trip_bindings_address", nil, nil},
		} {
			t.Run(driver+tc.code+tc.name, func(t *testing.T) {
				var err error = &pgconn.PgError{Code: tc.code, ConstraintName: tc.name}
				if driver == "pq" {
					err = &pq.Error{Code: pq.ErrorCode(tc.code), Constraint: tc.name}
				}
				err = fmt.Errorf("wrapped: %w", err)
				binding, deletion := tc.binding, tc.deletion
				if binding == nil {
					binding = err
				}
				if deletion == nil {
					deletion = err
				}
				require.ErrorIs(t, bindingError(err), binding)
				require.ErrorIs(t, addressDeleteError(err), deletion)
			})
		}
	}
}

func TestMutationTripLockModes(t *testing.T) {
	s, cleanup, id, a, _ := setupTrip(t)
	defer cleanup()
	p := s.(*pgTripDBWrapper)
	ctx := lockTestContext(t)
	owner := lockTestMember(t, p, ctx, id)
	recorder := &sqlRecorder{Interface: p.db.Logger}
	measured := &pgTripDBWrapper{db: p.db.Session(&gorm.Session{Logger: recorder})}
	// Keep the test recorder; the public constructor deliberately discards auth logs.
	auth := &pgAuthDBWrapper{db: measured.db}
	uid, _ := domain.UserIDFromContext(owner)
	for _, tc := range []struct {
		name, mode string
		run        func() error
	}{
		{"bind", "SHARE", func() error { return measured.Bind(owner, id, &a.ID) }},
		{"clear address", "SHARE", func() error { return measured.Bind(owner, id, nil) }},
		{"subscribe", "SHARE", func() error { return auth.SubscribeTrip(owner, uid, id) }},
		{"touch", "SHARE", func() error { return auth.TouchSubscribedTrip(owner, uid, id) }},
		{"create address", "SHARE", func() error {
			_, err := measured.CreateAddress(owner, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("new")}})
			return err
		}},
		{"update address", "SHARE", func() error {
			_, _, err := measured.UpdateAddress(owner, id, a.ID, domain.AddressOption{Name: addressNamePtr("renamed")})
			return err
		}},
		{"delete address", "SHARE", func() error { _, err := measured.DeleteAddress(owner, id, a.ID); return err }},
		{"private on", "UPDATE", func() error { _, err := measured.SetTripPrivate(owner, id, true); return err }},
		{"private off", "UPDATE", func() error { _, err := measured.SetTripPrivate(owner, id, false); return err }},
		{"rename", "UPDATE", func() error { return measured.UpdateTripInfo(owner, &domain.TripInfo{ID: id, Name: "renamed"}) }},
		{"unbind", "UPDATE", func() error { return measured.Unbind(owner, id) }},
		{"delete trip", "UPDATE", func() error { return measured.DeleteTrip(owner, id) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder.statements = nil
			require.NoError(t, tc.run())
			require.NotEmpty(t, recorder.statements)
			require.Contains(t, recorder.statements[0], `FROM "trips"`)
			require.Contains(t, recorder.statements[0], "FOR "+tc.mode)
		})
	}
}

func TestDeleteReferencedAddressReturnsConflict(t *testing.T) {
	s, cleanup, id, a, b := setupTrip(t)
	defer cleanup()
	ctx := lockTestContext(t)
	_, err := s.AppendNew(ctx, id, pgPayment(uuid.New(), a, b), testutil.Materializer{})
	require.NoError(t, err)
	for _, address := range []domain.Address{a, b} {
		_, err = s.DeleteAddress(ctx, id, address.ID)
		require.ErrorIs(t, err, domain.ErrConflict)
	}
	// Both failed transactions released their locks.
	require.NoError(t, s.DeleteTrip(ctx, id))
}
