package graph

import (
	"fmt"
	"net/http"
	"testing"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/domain"
	"dtm/graph/model"
	"dtm/libs/recordpatch"
	"github.com/99designs/gqlgen/client"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphQLHistoryAliasesAndCompletePayloads(t *testing.T) {
	store := mem.NewInMemoryTripDBWrapper()
	id := uuid.New()
	require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: id, Name: "history"}))
	payer, err := store.CreateAddress(id, "payer")
	require.NoError(t, err)
	member, err := store.CreateAddress(id, "member")
	require.NoError(t, err)
	ctx := resolverContext(store)
	base := resolverWithChain(store)
	factory := base.RecordFactory
	trip := base.TripFactory.ForTrip(id)
	value := domain.Record{RecordInfo: domain.RecordInfo{Name: "meal", Amount: 20, PrePayAddress: *payer}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *member}}}}
	intent, err := factory.New(ctx, id, value)
	require.NoError(t, err)
	root, err := trip.Append(ctx, intent)
	require.NoError(t, err)
	patch, err := recordpatch.Diff(domain.RecordFields{}, domain.RecordFields{Name: "updated"})
	require.NoError(t, err)
	intent, err = factory.Update(ctx, root.Record.ID(), patch)
	require.NoError(t, err)
	tail, err := trip.Append(ctx, intent)
	require.NoError(t, err)
	value.ID = uuid.New()
	value.IsDeleted = true
	_, err = store.AppendNew(ctx, id, value, seedRecordPolicy{})
	require.NoError(t, err)
	srv := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: base}))
	c := client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(db.WithTripDataLoader(r.Context(), db.NewTripDataLoader(store))))
	}))
	db.DataLoaderDebug.Reset()
	var metadata struct{ Trip struct{ Name string } }
	require.NoError(t, c.Post(fmt.Sprintf(`{ trip(tripId: %q) { name } }`, id), &metadata))
	assert.Zero(t, db.DataLoaderDebug.Snapshot().TripRecords.Batches)
	var response struct {
		Latest struct {
			Records []struct {
				ID                           string
				IsDeleted, IsValid, IsActive bool
				ShouldPayAddress             []struct{ ID string }
				ExtendPayMsg                 []float64
			}
			IsValid    bool
			MoneyShare []model.Tx
		}
		History struct {
			Records []struct {
				ID                  string
				IsDeleted, IsActive bool
			}
			IsValid    bool
			MoneyShare []model.Tx
		}
	}
	require.NoError(t, c.Post(fmt.Sprintf(`{
  latest: trip(tripId: %q) {
   records { id isDeleted isValid isActive shouldPayAddress { id } extendPayMsg }
   isValid moneyShare { input { amount address { id name } } output { amount address { id name } } }
  }
  history: trip(tripId: %q, haveHistory: true) {
   records { id isDeleted isActive }
   isValid moneyShare { input { amount address { id name } } output { amount address { id name } } }
  }
 }`, id, id), &response))
	require.Len(t, response.Latest.Records, 1)
	assert.Equal(t, tail.Record.ID().String(), response.Latest.Records[0].ID)
	assert.True(t, response.Latest.Records[0].IsValid)
	assert.True(t, response.Latest.Records[0].IsActive)
	require.Len(t, response.Latest.Records[0].ShouldPayAddress, 1)
	assert.Equal(t, member.ID.String(), response.Latest.Records[0].ShouldPayAddress[0].ID)
	assert.Equal(t, []float64{0}, response.Latest.Records[0].ExtendPayMsg)
	require.Len(t, response.History.Records, 3)
	assert.False(t, response.History.Records[0].IsActive)
	assert.True(t, response.History.Records[2].IsDeleted)
	assert.True(t, response.Latest.IsValid)
	assert.True(t, response.History.IsValid)
	assert.Equal(t, response.Latest.MoneyShare, response.History.MoneyShare)
	assert.Equal(t, int64(2), db.DataLoaderDebug.Snapshot().TripRecords.Batches)
	assert.Zero(t, db.DataLoaderDebug.Snapshot().Records.Batches, "nested fields must not reload individual records")
	// Full payloads no longer require any request reader.
	records, err := base.TripFactory.ForTrip(id).List(resolverContext(store))
	require.NoError(t, err)
	valid, err := records[0].Validate()
	require.NoError(t, err)
	assert.True(t, valid)
}
