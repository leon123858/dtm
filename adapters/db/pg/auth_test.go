package pg

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"dtm/adapters/db/internal/testutil"
	_ "dtm/migration"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthContract(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = CreateDSN()
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if db.PingContext(ctx) != nil {
		t.Skip("PostgreSQL unavailable")
	}
	schema := "auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	scoped := dsn
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		require.NoError(t, e)
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		scoped = u.String()
	} else {
		scoped += " search_path=" + schema
	}
	first, err := sql.Open("postgres", scoped)
	require.NoError(t, err)
	defer first.Close()
	second, err := sql.Open("postgres", scoped)
	require.NoError(t, err)
	defer second.Close()
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(first, "../../../migration"))
	firstORM, err := gorm.Open(postgres.New(postgres.Config{Conn: first}), &gorm.Config{})
	require.NoError(t, err)
	secondORM, err := gorm.Open(postgres.New(postgres.Config{Conn: second}), &gorm.Config{})
	require.NoError(t, err)
	var logs bytes.Buffer
	parentLogger := logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info})
	firstORM = firstORM.Session(&gorm.Session{Logger: parentLogger})
	authDB := NewPgAuthDBWrapper(firstORM).(*pgAuthDBWrapper)
	tripDB := NewPgTripDBWrapper(firstORM).(*pgTripDBWrapper)
	authPool, err := authDB.db.DB()
	require.NoError(t, err)
	tripPool, err := tripDB.db.DB()
	require.NoError(t, err)
	require.Same(t, tripPool, authPool)
	require.Same(t, parentLogger, firstORM.Logger)
	testutil.CheckAuthContract(t, authDB, NewPgAuthDBWrapper(secondORM))
	require.Empty(t, logs.String(), "auth queries must not reach the parent logger")
	testutil.CheckUserContract(t, authDB, NewPgAuthDBWrapper(secondORM), NewPgTripDBWrapper(firstORM.Session(&gorm.Session{Logger: logger.Discard})))
	require.Empty(t, logs.String(), "user queries must not reach the parent logger")
	require.NoError(t, goose.Down(first, "../../../migration"))
	var usersExist bool
	require.NoError(t, first.QueryRow(`SELECT to_regclass('users') IS NOT NULL`).Scan(&usersExist))
	require.True(t, usersExist)
	var columnExists bool
	require.NoError(t, first.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'trip_subscriptions' AND column_name = 'last_viewed_at')`).Scan(&columnExists))
	require.False(t, columnExists)
	uid, tid := uuid.New(), uuid.New()
	_, err = first.Exec(`INSERT INTO users(id,gcid) VALUES($1,$2)`, uid, uuid.NewString())
	require.NoError(t, err)
	_, err = first.Exec(`INSERT INTO trips(id,name) VALUES($1,'legacy')`, tid)
	require.NoError(t, err)
	_, err = first.Exec(`INSERT INTO trip_subscriptions(user_id,trip_id) VALUES($1,$2)`, uid, tid)
	require.NoError(t, err)
	var migrationStart time.Time
	require.NoError(t, first.QueryRow(`SELECT CURRENT_TIMESTAMP`).Scan(&migrationStart))
	require.NoError(t, goose.Up(first, "../../../migration"))
	var backfilled time.Time
	require.NoError(t, first.QueryRow(`SELECT last_viewed_at FROM trip_subscriptions WHERE user_id=$1 AND trip_id=$2`, uid, tid).Scan(&backfilled))
	require.False(t, backfilled.Before(migrationStart))
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	_, err = first.Exec(`UPDATE trip_subscriptions SET last_viewed_at=$1 WHERE user_id=$2 AND trip_id=$3`, future, uid, tid)
	require.NoError(t, err)
	require.NoError(t, authDB.TouchSubscribedTrip(context.Background(), uid, tid))
	subscriptions, err := authDB.SubscribedTrips(context.Background(), uid)
	require.NoError(t, err)
	require.Len(t, subscriptions, 1)
	require.True(t, subscriptions[0].LastViewedAt.Equal(future))

	require.NoError(t, goose.Down(first, "../../../migration"))
	var retained int
	require.NoError(t, first.QueryRow(`SELECT count(*) FROM trip_subscriptions WHERE user_id=$1 AND trip_id=$2`, uid, tid).Scan(&retained))
	require.Equal(t, 1, retained)
	require.NoError(t, goose.DownTo(first, "../../../migration", 20261023115959))
	var exists bool
	require.NoError(t, first.QueryRow(`SELECT to_regclass('auth_entries') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
}
