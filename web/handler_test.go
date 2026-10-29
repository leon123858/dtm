package web

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/db/testutil"
	"dtm/adapters/mq/goch"
	"dtm/config"
	"dtm/domain"
	"dtm/graph"
	"dtm/services/auth"
	tripservice "dtm/services/trip"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/99designs/gqlgen/graphql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResetTripDataLoaderAfterMutation(t *testing.T) {
	tests := []struct {
		name        string
		object      string
		resolverErr error
		wantBatches int64
	}{
		{name: "query keeps cache", object: "Query", wantBatches: 1},
		{name: "successful mutation resets cache", object: "Mutation", wantBatches: 2},
		{name: "failed mutation resets cache", object: "Mutation", resolverErr: errors.New("mutation failed"), wantBatches: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := mem.NewInMemoryTripDBWrapper()
			tripID := uuid.New()
			require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
			counts := new(testutil.LoaderCounters)
			loader := db.NewTripDataLoader(counts.Wrap(store))
			ctx := db.WithTripDataLoader(context.Background(), loader)
			ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{Object: tt.object})

			trip := tripservice.NewTripFactory(store, nil, func(context.Context) (db.Reader, error) { return loader, nil }).ForTrip(tripID)
			counts.Reset()
			_, err := trip.List(ctx)
			require.NoError(t, err)
			_, err = loader.LoadTrip(ctx, tripID)
			require.NoError(t, err)
			_, err = loader.LoadTrip(ctx, tripID)
			require.NoError(t, err)
			assert.Equal(t, testutil.DataLoadCount{Batches: 1, Keys: 1}, counts.Snapshot().Trips)

			_, err = resetTripDataLoaderAfterMutation(ctx, func(context.Context) (any, error) {
				return nil, tt.resolverErr
			})
			require.ErrorIs(t, err, tt.resolverErr)

			_, err = loader.LoadTrip(ctx, tripID)
			require.NoError(t, err)
			assert.Equal(t, testutil.DataLoadCount{Batches: tt.wantBatches, Keys: tt.wantBatches}, counts.Snapshot().Trips)
			_, err = trip.List(ctx)
			require.NoError(t, err)
			assert.Equal(t, testutil.DataLoadCount{Batches: tt.wantBatches, Keys: tt.wantBatches}, counts.Snapshot().TripRecords)
		})
	}
}

func TestPrivateTripGraphQL(t *testing.T) {
	ctx := context.Background()
	store, users := mem.NewInMemoryDB()
	u, err := users.UpsertUser(ctx, "member")
	require.NoError(t, err)
	v, err := users.UpsertUser(ctx, "outsider")
	require.NoError(t, err)
	service, err := auth.NewServiceWithDependencies(ctx, config.Auth{}, auth.Dependencies{Store: users})
	require.NoError(t, err)
	defer service.Close()
	readers := func(ctx context.Context) (db.Reader, error) { return db.TripDataLoaderFromContext(ctx) }
	resolver := &graph.Resolver{Auth: service, TripFactory: tripservice.NewTripFactory(store, users, readers), RecordFactory: tripservice.NewRecordFactory(readers), TripMessageQueueWrapper: goch.NewGoChanTripMessageQueueWrapper()}
	router := gin.New()
	router.POST("/query", TripDataLoaderInjectionMiddleware(store), GraphQLHandler(graph.NewExecutableSchema(graph.Config{Resolvers: resolver})))
	request := func(uid uuid.UUID, query string) map[string]any {
		body, err := json.Marshal(map[string]string{"query": query})
		require.NoError(t, err)
		req := httptest.NewRequest("POST", "/query", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		if uid != uuid.Nil {
			req = req.WithContext(domain.WithUserID(req.Context(), uid))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, 200, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	success := func(uid uuid.UUID, q string) map[string]any {
		out := request(uid, q)
		require.Nil(t, out["errors"], out)
		return out["data"].(map[string]any)
	}
	code := func(uid uuid.UUID, q, want string) {
		out := request(uid, q)
		require.NotNil(t, out["errors"], out)
		e := out["errors"].([]any)[0].(map[string]any)
		require.Equal(t, want, e["extensions"].(map[string]any)["code"], out)
	}
	created := success(u.ID, `mutation { createTrip(input:{name:"private test"}){id private} }`)["createTrip"].(map[string]any)
	id := created["id"].(string)
	require.Equal(t, false, created["private"])
	trip := success(u.ID, fmt.Sprintf(`{trip(tripId:%q){users{userId}}}`, id))["trip"].(map[string]any)
	require.Empty(t, trip["users"])
	address := success(uuid.Nil, fmt.Sprintf(`mutation {createAddress(tripId:%q,input:{name:"payer"}){id}}`, id))["createAddress"].(map[string]any)["id"].(string)
	code(u.ID, fmt.Sprintf(`mutation {updateTrip(tripId:%q,input:{private:true}){id}}`, id), "FORBIDDEN")
	success(v.ID, fmt.Sprintf(`mutation {subscribeTrip(tripId:%q){id}}`, id))
	record := success(uuid.Nil, fmt.Sprintf(`mutation {createRecord(tripId:%q,input:{name:"secret",amount:50,time:"1700000000000",prePayAddressId:%q,shouldPayAddressIds:[%q],category:NORMAL,extendPayMsg:[]}){id}}`, id, address, address))["createRecord"].(map[string]any)["id"].(string)
	// Sequential mutations use fresh authorization and binding reads, including nested statistics.
	data := success(u.ID, fmt.Sprintf(`mutation {
 join:bind(tripId:%q,addressId:%q){id}
 lock:updateTrip(tripId:%q,input:{name:"renamed private",private:true}){name private}
 clear:bind(tripId:%q){id}
 }`, id, address, id, id))
	require.Equal(t, true, data["lock"].(map[string]any)["private"])
	require.Equal(t, "renamed private", data["lock"].(map[string]any)["name"])
	require.Equal(t, u.ID.String(), data["clear"].(map[string]any)["id"])
	success(u.ID, fmt.Sprintf(`mutation {bind(tripId:%q,addressId:%q){id}}`, id, address))
	success(u.ID, fmt.Sprintf(`mutation {bind(tripId:%q,addressId:null){id}}`, id))
	code(u.ID, fmt.Sprintf(`mutation {unbind(tripId:%q){id}}`, id), "CONFLICT")
	for _, uid := range []uuid.UUID{uuid.Nil, v.ID} {
		want := "FORBIDDEN"
		if uid == uuid.Nil {
			want = "UNAUTHENTICATED"
		}
		code(uid, fmt.Sprintf(`mutation {updateRecord(recordId:%q,input:{old:{name:"secret",amount:50,prePayAddressId:%q,shouldPayAddressIds:[%q]},new:{name:"denied",amount:50,prePayAddressId:%q,shouldPayAddressIds:[%q]}}){id name}}`, record, address, address, address, address), want)

		code(uid, fmt.Sprintf(`{trip(tripId:%q,haveHistory:true){name private addresses{id statistics{totalExpense}} records{id} moneyShare{output{amount}}}}`, id), want)
		code(uid, fmt.Sprintf(`mutation {updateTrip(tripId:%q,input:{name:"denied"}){id}}`, id), want)
		code(uid, fmt.Sprintf(`mutation {createAddress(tripId:%q,input:{name:"denied"}){id}}`, id), want)
		code(uid, fmt.Sprintf(`mutation {updateAddress(tripId:%q,addressId:%q,input:{name:"denied"}){id}}`, id, address), want)
		code(uid, fmt.Sprintf(`mutation {deleteAddress(tripId:%q,addressId:%q){id}}`, id, address), want)
		code(uid, fmt.Sprintf(`mutation {bind(tripId:%q){id}}`, id), want)
	}
	success(u.ID, fmt.Sprintf(`{trip(tripId:%q){private name addresses{id statistics{totalExpense}} records{id}}}`, id))
	outsider := success(v.ID, `{me{subscribedTrips{id name}}}`)["me"].(map[string]any)
	require.Len(t, outsider["subscribedTrips"], 1)
	code(v.ID, fmt.Sprintf(`mutation {subscribeTrip(tripId:%q){id}}`, id), "FORBIDDEN")
	code(v.ID, fmt.Sprintf(`mutation {touchSubscribedTrip(tripId:%q){id}}`, id), "FORBIDDEN")
	success(v.ID, fmt.Sprintf(`mutation {unsubscribeTrip(tripId:%q){id}}`, id))
	success(v.ID, fmt.Sprintf(`mutation {unbind(tripId:%q){id}}`, id))
	code(u.ID, `mutation {bind(tripId:"invalid"){id}}`, "BAD_USER_INPUT")
	success(u.ID, fmt.Sprintf(`mutation {updateTrip(tripId:%q,input:{private:false}){private} unbind(tripId:%q){id}}`, id, id))
	success(uuid.Nil, fmt.Sprintf(`{trip(tripId:%q){name private}}`, id))
}
