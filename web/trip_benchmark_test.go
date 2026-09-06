package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/pg"
	"dtm/domain"
	"dtm/graph"
	_ "dtm/migration"
	tripservice "dtm/services/trip"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Dedicated connection logger: does not retain SQL or serialize workers on a log writer.
type tripSQLCounter struct {
	gormlogger.Interface
	selects  atomic.Int64
	failures atomic.Int64
}

func (c *tripSQLCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), err error) {
	statement, _ := fc()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "SELECT") {
		c.selects.Add(1)
	}
	if err != nil {
		c.failures.Add(1)
	}
}

type tripBenchFixture struct {
	id            uuid.UUID
	live          int
	payer, member uuid.UUID
}
type tripBenchHarness struct {
	store   db.TripDBWrapper
	sqlDB   *sql.DB
	counter *tripSQLCounter
}

func newTripBenchHarness(t testing.TB) *tripBenchHarness {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost user=postgres dbname=postgres port=5432 sslmode=disable"
	}
	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnectTimeout = 5 * time.Second
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	require.NoError(t, admin.Ping(), "benchmark requires PostgreSQL")
	schema := "trip_bench_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); require.NoError(t, err) })
	cfg.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(32)
	pool.SetMaxIdleConns(32)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.UpContext(context.Background(), pool, "../migration"))
	counter := &tripSQLCounter{Interface: gormlogger.Discard}
	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: counter})
	require.NoError(t, err)
	var version string
	require.NoError(t, pool.QueryRow("SHOW server_version").Scan(&version))
	t.Logf("environment go=%s os=%s arch=%s CPUs=%d GOMAXPROCS=%d postgres=%s pool=32", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0), version)
	return &tripBenchHarness{pg.NewPgDBWrapper(orm), pool, counter}
}

func (h *tripBenchHarness) seed(t testing.TB, n int) tripBenchFixture {
	t.Helper()
	id, payer, member := uuid.New(), uuid.New(), uuid.New()
	tx, err := h.sqlDB.Begin()
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO trips(id,name) VALUES($1,'benchmark')", id)
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO addresses(id,trip_id,name) VALUES($1,$3,'payer'),($2,$3,'member')", payer, member, id)
	require.NoError(t, err)
	// One linked historical predecessor and one deleted standalone record, in addition to n live tails.
	root, tail := uuid.New(), uuid.New()
	for i := 0; i < n+2; i++ {
		recordID := uuid.New()
		var parent, child *uuid.UUID
		if i == 0 {
			recordID = tail
			parent = &root
		}
		if i == n {
			recordID = root
			child = &tail
		}
		_, err = tx.Exec(`INSERT INTO records(id,trip_id,name,amount,time,pre_pay_address_id,category,parent_record_id,child_record_id,is_deleted) VALUES($1,$2,'meal',20,'2026-01-01T00:00:00Z',$3,0,$4,$5,$6)`, recordID, id, payer, parent, child, i == n+1)
		require.NoError(t, err)
		_, err = tx.Exec("INSERT INTO record_should_pay_address_lists(record_id,trip_id,address_id,extended_msg) VALUES($1,$2,$3,0)", recordID, id, member)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	return tripBenchFixture{id, n, payer, member}
}

// Negative control: every Reader call gets a fresh loader and therefore no shared cache.
// Uses the same instrumentation as the cached path; timings are not compared.
type uncachedTripReader struct{ store db.DataLoaderStore }

func (r uncachedTripReader) LoadTrip(c context.Context, id uuid.UUID) (*domain.TripInfo, error) {
	return db.NewTripDataLoader(r.store).LoadTrip(c, id)
}
func (r uncachedTripReader) LoadRecord(c context.Context, id uuid.UUID) (db.RecordSnapshot, error) {
	return db.NewTripDataLoader(r.store).LoadRecord(c, id)
}
func (r uncachedTripReader) LoadTripRecords(c context.Context, id uuid.UUID, o db.RecordReadOptions) ([]db.RecordSnapshot, error) {
	return db.NewTripDataLoader(r.store).LoadTripRecords(c, id, o)
}
func (r uncachedTripReader) LoadTripAddresses(c context.Context, id uuid.UUID) ([]domain.Address, error) {
	return db.NewTripDataLoader(r.store).LoadTripAddresses(c, id)
}

func (h *tripBenchHarness) server(t testing.TB, cached bool) *httptest.Server {
	t.Helper()
	provider := func(ctx context.Context) (db.Reader, error) {
		if !cached {
			return uncachedTripReader{h.store}, nil
		}
		return db.TripDataLoaderFromContext(ctx)
	}
	resolver := &graph.Resolver{TripFactory: tripservice.NewTripFactory(h.store, provider), RecordFactory: tripservice.NewRecordFactory(provider)}
	router := gin.New()
	router.POST("/query", TripDataLoaderInjectionMiddleware(h.store), GraphQLHandler(graph.NewExecutableSchema(graph.Config{Resolvers: resolver})))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

type tripBenchScenario struct {
	name                      string
	aliases                   int
	history, metadata         bool
	trips, records, addresses int64
}

var tripBenchScenarios = []tripBenchScenario{
	{name: "metadata", aliases: 1, metadata: true, trips: 1},
	{name: "full", aliases: 1, trips: 1, records: 1, addresses: 1},
	{name: "aliases8", aliases: 8, trips: 1, records: 1, addresses: 1},
	{name: "history", aliases: 2, history: true, trips: 1, records: 2, addresses: 1},
}

const tripBenchFields = `id name records { id amount prePayAddress { id name } shouldPayAddress { id name } extendPayMsg parentRecordId isDeleted isActive isValid } addresses { id name } moneyShare { input { amount address { id name } } output { amount address { id name } } } isValid`

func (s tripBenchScenario) body(f tripBenchFixture) []byte {
	var query strings.Builder
	query.WriteString("{")
	for i := 0; i < s.aliases; i++ {
		fields := tripBenchFields
		if s.metadata {
			fields = "id name"
		}
		fmt.Fprintf(&query, "a%d:trip(tripId:%q,haveHistory:%t){%s}", i, f.id, s.history && i == 1, fields)
	}
	query.WriteString("}")
	body, _ := json.Marshal(map[string]string{"query": query.String()})
	return body
}

type tripBenchAddress struct{ ID, Name string }
type tripBenchPayment struct {
	Amount  float64
	Address tripBenchAddress
}
type tripBenchResult struct {
	ID, Name string
	Records  []struct {
		ID                           string
		Amount                       float64
		PrePayAddress                tripBenchAddress
		ShouldPayAddress             []tripBenchAddress
		ExtendPayMsg                 []float64
		ParentRecordID               *string
		IsDeleted, IsActive, IsValid bool
	}
	Addresses  []tripBenchAddress
	MoneyShare []struct {
		Input  []tripBenchPayment
		Output tripBenchPayment
	}
	IsValid bool
}

func validateTripBench(body []byte, s tripBenchScenario, f tripBenchFixture) error {
	var response struct {
		Data   map[string]*tripBenchResult
		Errors []json.RawMessage
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("GraphQL errors: %s", response.Errors)
	}
	if len(response.Data) != s.aliases {
		return fmt.Errorf("got %d aliases", len(response.Data))
	}
	for i := 0; i < s.aliases; i++ {
		result := response.Data[fmt.Sprintf("a%d", i)]
		if result == nil || result.ID != f.id.String() || result.Name != "benchmark" {
			return fmt.Errorf("incorrect trip")
		}
		if s.metadata {
			continue
		}
		expected := f.live
		if s.history && i == 1 {
			expected += 2
		}
		if len(result.Records) != expected || len(result.Addresses) != 2 || !result.IsValid {
			return fmt.Errorf("invalid payload records=%d expected=%d valid=%t", len(result.Records), expected, result.IsValid)
		}
		active, deleted, parents := 0, 0, 0
		seen := make(map[string]bool, len(result.Records))
		for _, r := range result.Records {
			if seen[r.ID] {
				return fmt.Errorf("duplicate record")
			}
			seen[r.ID] = true
			if r.IsActive {
				active++
			}
			if r.IsDeleted {
				deleted++
			}
			if r.ParentRecordID != nil {
				parents++
			}
			if !r.IsValid || r.Amount != 20 || r.PrePayAddress.ID != f.payer.String() || len(r.ShouldPayAddress) != 1 || r.ShouldPayAddress[0].ID != f.member.String() || len(r.ExtendPayMsg) != 1 || r.ExtendPayMsg[0] != 0 {
				return fmt.Errorf("incorrect nested record")
			}
		}
		wantDeleted := 0
		if s.history && i == 1 {
			wantDeleted = 1
		}
		// IsActive means canonical chain tail, including a deleted tail.
		if active != f.live+wantDeleted || deleted != wantDeleted || parents != 1 {
			return fmt.Errorf("incorrect history active=%d deleted=%d parents=%d", active, deleted, parents)
		}
		if len(result.MoneyShare) != 1 {
			return fmt.Errorf("incorrect settlement count")
		}
		settlement := result.MoneyShare[0]
		if settlement.Output.Amount != float64(20*f.live) || settlement.Output.Address.ID != f.payer.String() || len(settlement.Input) != 1 || settlement.Input[0].Address.ID != f.member.String() || settlement.Input[0].Amount != float64(20*f.live) {
			return fmt.Errorf("incorrect settlement: %+v", settlement)
		}
	}
	return nil
}

type tripBenchReport struct {
	Scenario                          string `json:"scenario"`
	Records, Concurrency, Requests    int
	Seconds, RPS, P50ms, P95ms, P99ms float64
	Errors                            int64
	SQL                               int64
	Fetches                           db.DataLoaderDebugSnapshot
	PoolWaitCount                     int64
	PoolWaitMS                        float64
}

func (h *tripBenchHarness) run(t testing.TB, url string, s tripBenchScenario, f tripBenchFixture, workers, requests int, cached bool) tripBenchReport {
	t.Helper()
	transport := &http.Transport{MaxIdleConns: 256, MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	body := s.body(f)
	call := func() error {
		response, err := client.Post(url+"/query", "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", response.StatusCode)
		}
		return validateTripBench(data, s, f)
	}
	require.NoError(t, call(), "warmup")
	db.DataLoaderDebug.Reset()
	h.counter.selects.Store(0)
	h.counter.failures.Store(0)
	before := h.sqlDB.Stats()
	latencies := make([]time.Duration, requests)
	var next, failures atomic.Int64
	var firstErr error
	var once sync.Once
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				i := int(next.Add(1) - 1)
				if i >= requests {
					return
				}
				began := time.Now()
				err := call()
				latencies[i] = time.Since(began)
				if err != nil {
					failures.Add(1)
					once.Do(func() { firstErr = err })
				}
			}
		}()
	}
	if b, ok := t.(*testing.B); ok {
		b.StartTimer()
	}
	began := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(began)
	if b, ok := t.(*testing.B); ok {
		b.StopTimer()
	}
	after := h.sqlDB.Stats()
	got := db.DataLoaderDebug.Snapshot()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	percentile := func(p int) float64 { return float64(latencies[(requests*p+99)/100-1]) / float64(time.Millisecond) }
	report := tripBenchReport{Scenario: s.name, Records: f.live, Concurrency: workers, Requests: requests, Seconds: elapsed.Seconds(), RPS: float64(requests) / elapsed.Seconds(), P50ms: percentile(50), P95ms: percentile(95), P99ms: percentile(99), Errors: failures.Load(), SQL: h.counter.selects.Load(), Fetches: got, PoolWaitCount: after.WaitCount - before.WaitCount, PoolWaitMS: float64(after.WaitDuration-before.WaitDuration) / float64(time.Millisecond)}
	raw, _ := json.Marshal(report)
	t.Logf("TRIP_BENCH %s", raw)
	require.NoError(t, firstErr)
	require.Zero(t, failures.Load())
	require.Zero(t, h.counter.failures.Load())
	n := int64(requests)
	want := db.DataLoaderDebugSnapshot{Trips: db.DataLoadCount{Batches: s.trips * n, Keys: s.trips * n}, TripRecords: db.DataLoadCount{Batches: s.records * n, Keys: s.records * n}, TripAddresses: db.DataLoadCount{Batches: s.addresses * n, Keys: s.addresses * n}}
	if !cached {
		want.Trips = db.DataLoadCount{Batches: int64(s.aliases) * n, Keys: int64(s.aliases) * n}
		want.TripAddresses = want.Trips
		want.TripRecords = db.DataLoadCount{Batches: int64(3*s.aliases) * n, Keys: int64(3*s.aliases) * n}
	}
	require.Equal(t, want, got, "backing fetch budget")
	require.Equal(t, want.Trips.Batches+want.TripRecords.Batches+want.TripAddresses.Batches, report.SQL, "actual SELECT budget")
	return report
}

// Opt-in: creates and drops only a unique benchmark schema, never application tables.
func TestTripQueryPostgresLoad(t *testing.T) {
	if os.Getenv("DTM_TRIP_BENCH") != "1" {
		t.Skip("set DTM_TRIP_BENCH=1 to run the PostgreSQL load experiment")
	}
	h := newTripBenchHarness(t)
	server := h.server(t, true)
	for _, records := range []int{10, 1000} {
		f := h.seed(t, records)
		for _, scenario := range tripBenchScenarios {
			for _, workers := range []int{1, 32, 128, 256} {
				for round := 1; round <= 3; round++ {
					if !t.Run(fmt.Sprintf("records%d/%s/workers%d/round%d", records, scenario.name, workers, round), func(t *testing.T) { h.run(t, server.URL, scenario, f, workers, 1024, true) }) {
						return
					}
				}
			}
		}
	}
	t.Run("uncached_control", func(t *testing.T) {
		server := h.server(t, false)
		f := h.seed(t, 10)
		h.run(t, server.URL, tripBenchScenarios[2], f, 32, 128, false)
	})
}

func BenchmarkTripQueryPostgres(b *testing.B) {
	if os.Getenv("DTM_TRIP_BENCH") != "1" {
		b.Skip("set DTM_TRIP_BENCH=1 to run PostgreSQL benchmark")
	}
	h := newTripBenchHarness(b)
	server := h.server(b, true)
	f := h.seed(b, 10)
	for _, workers := range []int{1, 32, 128, 256} {
		b.Run(fmt.Sprint(workers), func(b *testing.B) {
			b.StopTimer()
			// run reports its own measured duration, excluding fixture setup and warmup.
			report := h.run(b, server.URL, tripBenchScenarios[2], f, workers, b.N, true)
			b.ReportMetric(report.Seconds*1e9/float64(b.N), "ns/op")
			b.ReportMetric(float64(report.SQL)/float64(b.N), "SELECT/request")
			b.ReportMetric(report.P95ms, "p95-ms")
		})
	}
}
