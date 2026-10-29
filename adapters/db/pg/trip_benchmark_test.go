package pg

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"dtm/adapters/db/testutil"
	"dtm/domain"
	"dtm/libs/recordpatch"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Opt-in because this resets the test database, like the integration suite.
func TestTripLockLoad(t *testing.T) {
	if os.Getenv("DTM_LOCK_BENCH") != "1" {
		t.Skip("set DTM_LOCK_BENCH=1")
	}
	for _, workload := range []string{"different-records", "same-chain", "different-trips"} {
		for _, clients := range []int{1, 8, 32} {
			t.Run(fmt.Sprintf("%s/%d", workload, clients), func(t *testing.T) {
				store, cleanup := setupTestDB(t)
				defer cleanup()
				sqlDB, err := store.(*pgTripDBWrapper).db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(40)
				sqlDB.SetMaxIdleConns(40)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				// Identical initial data and pool limit for every run and implementation.
				trips := make([]uuid.UUID, 32)
				roots := make([]uuid.UUID, 32)
				for i := range trips {
					trips[i] = uuid.New()
					require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: trips[i], Name: "load"}))
				}
				for i := range roots {
					tid := trips[0]
					if workload == "different-trips" {
						tid = trips[i]
					}
					a, err := store.CreateAddress(ctx, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr(fmt.Sprintf("a%d", i))}})
					require.NoError(t, err)
					roots[i] = uuid.New()
					_, err = store.AppendNew(ctx, tid, pgPayment(roots[i], *a, *a), testutil.Materializer{})
					require.NoError(t, err)
				}
				const rounds = 30
				latencies := make(chan time.Duration, clients*rounds)
				errs := make(chan error, clients)
				start := make(chan struct{})
				var wg sync.WaitGroup
				for c := 0; c < clients; c++ {
					wg.Add(1)
					go func(c int) {
						defer wg.Done()
						<-start
						tid, target := trips[0], roots[c]
						if workload == "different-trips" {
							tid = trips[c]
						}
						if workload == "same-chain" {
							target = roots[0]
						}
						for n := 0; n < rounds; n++ {
							patch, e := recordpatch.Diff(domain.RecordFields{}, domain.RecordFields{Name: fmt.Sprintf("%d-%d", c, n)})
							if e != nil {
								errs <- e
								return
							}
							before := time.Now()
							_, record, _, e := store.AppendPatch(ctx, tid, target, patch, testutil.Materializer{})
							latencies <- time.Since(before)
							if e != nil {
								errs <- e
								return
							}
							target = record.ID
						}
					}(c)
				}
				before := time.Now()
				close(start)
				wg.Wait()
				elapsed := time.Since(before)
				close(errs)
				close(latencies)
				for e := range errs {
					require.NoError(t, e)
				}
				samples := []time.Duration{}
				for d := range latencies {
					samples = append(samples, d)
				}
				sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
				t.Logf("clients=%d ops=%d throughput=%.1f/s p95=%.2fms", clients, len(samples), float64(len(samples))/elapsed.Seconds(), float64(samples[(len(samples)*95-1)/100])/float64(time.Millisecond))
			})
		}
	}
}

func addressNamePtr(name string) *string { return &name }
