package web

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/identity"
	"dtm/domain"
	"dtm/graph"
	"dtm/services/auth"
	tripservice "dtm/services/trip"

	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testVerifier struct{ calls atomic.Int64 }

func (v *testVerifier) Verify(_ context.Context, raw string) (string, error) {
	v.calls.Add(1)
	switch raw {
	case "jwt-secret":
		return "member", nil
	case "other":
		return "other", nil
	case "missing":
		return "missing", nil
	case "unavailable":
		return "", identity.ErrKeysUnavailable
	}
	return "", identity.ErrToken
}
func TestMemberHTTPFlow(t *testing.T) {
	t.Setenv("ADMIN_KEY", "")
	trips, store := mem.NewInMemoryDB()
	router, authService := testAuth(t, auth.Dependencies{Store: store})
	verifier := &testVerifier{}
	schema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		Auth:        authService,
		TripFactory: tripservice.NewTripFactory(trips, store, func(ctx context.Context) (db.Reader, error) { return db.TripDataLoaderFromContext(ctx) }),
	}})
	router.POST("/query", UserBearerExtractIdMiddleware(verifier, store), TripDataLoaderInjectionMiddleware(trips), GraphQLHandler(schema))
	query := func(token, q string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]string{"query": q})
		req := httptest.NewRequest("POST", "/query", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return w.Code, out
	}
	_, out := query("", "{me{id}}")
	require.Nil(t, out["data"].(map[string]any)["me"])
	_, out = query("", "mutation{subscribeTrip(tripId:\"bad\"){id}}")
	require.Contains(t, out["errors"], map[string]any{"message": "UNAUTHENTICATED", "path": []any{"subscribeTrip"}, "extensions": map[string]any{"code": "UNAUTHENTICATED"}})
	status, _ := query("jwt-secret", "{me{id}}")
	require.Equal(t, 401, status)
	// Complete OAuth before the token can resolve to a local member.
	start := perform(router, "GET", "/auth/google", "", nil)
	loc, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	callback := perform(router, "GET", "/auth/callback?state="+loc.Query().Get("state")+"&code=provider-code", "", start.Result().Cookies()[0])
	loc, err = url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	exchange := perform(router, "POST", "/auth/exchange", `{"code":"`+loc.Query().Get("code")+`"}`, nil)
	require.Equal(t, 200, exchange.Code)
	require.NotContains(t, exchange.Body.String(), "localId")
	before := verifier.calls.Load()
	_, out = query("jwt-secret", "{a:me{id subscribedTrips{id name}} b:me{id}}")
	require.Nil(t, out["errors"])
	require.Equal(t, before+1, verifier.calls.Load())
	me := out["data"].(map[string]any)["a"].(map[string]any)
	require.Empty(t, me["subscribedTrips"])
	_, out = query("jwt-secret", "mutation{createTrip(input:{name:\"saved\"}){id name}}")
	require.Nil(t, out["errors"])
	tid := out["data"].(map[string]any)["createTrip"].(map[string]any)["id"].(string)
	_, out = query("jwt-secret", "{me{subscribedTrips{id name}}}")
	require.Nil(t, out["errors"])
	require.Len(t, out["data"].(map[string]any)["me"].(map[string]any)["subscribedTrips"], 1)

	viewed := func(operation string, result map[string]any) string {
		require.Nil(t, result["errors"])
		return result["data"].(map[string]any)[operation].(map[string]any)["subscribedTrips"].([]any)[0].(map[string]any)["lastViewedAt"].(string)
	}
	_, out = query("jwt-secret", "{me{subscribedTrips{id name lastViewedAt}}}")
	initial := viewed("me", out)
	millis, err := strconv.ParseInt(initial, 10, 64)
	require.NoError(t, err)
	require.Positive(t, millis)
	require.Equal(t, strconv.FormatInt(millis, 10), initial)
	_, out = query("jwt-secret", `{me{subscribedTrips{lastViewedAt}}}`)
	require.Equal(t, initial, viewed("me", out))
	_, out = query("jwt-secret", `mutation{subscribeTrip(tripId:"`+tid+`"){subscribedTrips{lastViewedAt}}}`)
	require.Equal(t, initial, viewed("subscribeTrip", out))
	_, out = query("jwt-secret", `mutation{updateTrip(tripId:"`+tid+`",input:{name:"renamed"}){id}}`)
	require.Nil(t, out["errors"])
	_, out = query("jwt-secret", `{me{subscribedTrips{lastViewedAt}}}`)
	require.Equal(t, initial, viewed("me", out))
	time.Sleep(2 * time.Millisecond)
	_, out = query("jwt-secret", `mutation{touchSubscribedTrip(tripId:"`+tid+`"){id subscribedTrips{id name lastViewedAt}}}`)
	touched, err := strconv.ParseInt(viewed("touchSubscribedTrip", out), 10, 64)
	require.NoError(t, err)
	require.Greater(t, touched, millis)
	for _, tc := range []struct{ token, id, code string }{
		{"", tid, "UNAUTHENTICATED"},
		{"jwt-secret", "bad", "BAD_USER_INPUT"},
		{"jwt-secret", uuid.NewString(), "NOT_FOUND"},
	} {
		_, out = query(tc.token, `mutation{touchSubscribedTrip(tripId:"`+tc.id+`"){id}}`)
		require.Equal(t, tc.code, out["errors"].([]any)[0].(map[string]any)["extensions"].(map[string]any)["code"])
	}
	_, err = store.UpsertUser(context.Background(), "other")
	require.NoError(t, err)
	_, out = query("other", "{me{subscribedTrips{id}}}")
	require.Empty(t, out["data"].(map[string]any)["me"].(map[string]any)["subscribedTrips"])

	_, out = query("other", `mutation{touchSubscribedTrip(tripId:"`+tid+`"){id}}`)
	require.Equal(t, "NOT_FOUND", out["errors"].([]any)[0].(map[string]any)["extensions"].(map[string]any)["code"])
	_, out = query("other", "{me{subscribedTrips{id}}}")
	require.Empty(t, out["data"].(map[string]any)["me"].(map[string]any)["subscribedTrips"])
	for range 2 {
		_, out = query("other", `mutation{subscribeTrip(tripId:"`+tid+`"){subscribedTrips{id}}}`)
		require.Nil(t, out["errors"])
		require.Len(t, out["data"].(map[string]any)["subscribeTrip"].(map[string]any)["subscribedTrips"], 1)
		_, out = query("jwt-secret", `mutation{unsubscribeTrip(tripId:"`+tid+`"){subscribedTrips{id}}}`)
		require.Nil(t, out["errors"])
	}
	for _, tc := range []struct{ id, code string }{{"bad", "BAD_USER_INPUT"}, {uuid.NewString(), "NOT_FOUND"}} {
		_, out = query("jwt-secret", `mutation{subscribeTrip(tripId:"`+tc.id+`"){id}}`)
		require.Equal(t, tc.code, out["errors"].([]any)[0].(map[string]any)["extensions"].(map[string]any)["code"])
	}
	_, out = query("", "mutation{createTrip(input:{name:\"anonymous\"}){id}}")
	require.Nil(t, out["errors"])
	for _, tc := range []struct {
		token  string
		status int
	}{{"invalid", 401}, {"missing", 401}, {"unavailable", 503}} {
		status, _ = query(tc.token, "mutation{createTrip(input:{name:\"rejected\"}){id}}")
		require.Equal(t, tc.status, status)
	}
	_, out = query("other", "{me{subscribedTrips{id}}}")
	require.Len(t, out["data"].(map[string]any)["me"].(map[string]any)["subscribedTrips"], 1)
}

type failingUsers struct{ db.AuthDBWrapper }

func (failingUsers) UpsertUser(context.Context, string) (u domain.User, err error) {
	return u, errors.New("storage failed")
}
func TestMemberStorageFailureDoesNotIssueCode(t *testing.T) {
	router, _ := testAuth(t, auth.Dependencies{Store: failingUsers{AuthDBWrapper: mem.NewInMemoryAuthDBWrapper()}})
	start := perform(router, "GET", "/auth/google", "", nil)
	loc, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	callback := perform(router, "GET", "/auth/callback?state="+loc.Query().Get("state")+"&code=provider-code", "", start.Result().Cookies()[0])
	loc, err = url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "storage_error", loc.Query().Get("error"))
	require.Empty(t, loc.Query().Get("code"))
	refresh := perform(router, "POST", "/auth/refresh", `{"refreshToken":"refresh"}`, nil)
	require.Equal(t, 200, refresh.Code)
}

func TestInvalidTokenNeverExecutesHandler(t *testing.T) {
	store := mem.NewInMemoryAuthDBWrapper()
	router, _ := testAuth(t, auth.Dependencies{Store: store})
	verifier := &testVerifier{}
	calls := 0
	router.POST("/query", UserBearerExtractIdMiddleware(verifier, store), func(c *gin.Context) { calls++; c.Status(200) })
	for _, header := range []string{"Bearer invalid", "Bearer missing", "Bearer unavailable", "Basic invalid", "Bearer", ""} {
		req := httptest.NewRequest("POST", "/query", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Contains(t, []int{401, 503}, w.Code)
	}
	require.Zero(t, calls)
}

// WebSocket upgrades preserve the identity established by the bearer middleware.
func TestWebSocketRetainsMemberIdentity(t *testing.T) {
	store := mem.NewInMemoryAuthDBWrapper()
	router, s := testAuth(t, auth.Dependencies{Store: store})
	member, err := store.UpsertUser(context.Background(), "member")
	require.NoError(t, err)
	schema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{Auth: s}})
	router.GET("/query", UserBearerExtractIdMiddleware(&testVerifier{}, store), GraphQLHandler(schema))
	server := httptest.NewServer(router)
	defer server.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/query", http.Header{"Authorization": []string{"Bearer jwt-secret"}})
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, conn.WriteJSON(map[string]any{"type": "connection_init"}))
	var message map[string]any
	require.NoError(t, conn.ReadJSON(&message))
	require.Equal(t, "connection_ack", message["type"])
	require.NoError(t, conn.WriteJSON(map[string]any{"id": "1", "type": "subscribe", "payload": map[string]any{"query": "{me{id}}"}}))
	require.NoError(t, conn.ReadJSON(&message))
	require.Equal(t, "next", message["type"])
	payload := message["payload"].(map[string]any)
	require.Nil(t, payload["errors"])
	require.Equal(t, member.ID.String(), payload["data"].(map[string]any)["me"].(map[string]any)["id"])
}

// Count identity mapping calls and verify the resolved UUID reaches the handler.
type identityMappingOnly struct {
	db.AuthDBWrapper
	id    uuid.UUID
	err   error
	calls int
}

func (s *identityMappingOnly) UserGCID2UserId(context.Context, string) (uuid.UUID, error) {
	s.calls++
	return s.id, s.err
}

func TestUserBearerExtractIdMiddlewareResolvesIdentityOnceAndInjectsUUID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "authenticated", status: http.StatusNoContent},
		{name: "unknown user", err: domain.ErrUserNotFound, status: http.StatusUnauthorized},
		{name: "storage failure", err: errors.New("storage unavailable"), status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &identityMappingOnly{id: uuid.New(), err: tc.err}
			router := gin.New()
			handled := false
			router.GET("/", UserBearerExtractIdMiddleware(&testVerifier{}, store), func(c *gin.Context) {
				handled = true
				id, ok := domain.UserIDFromContext(c.Request.Context())
				require.True(t, ok)
				require.Equal(t, store.id, id)
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer jwt-secret")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, 1, store.calls)
			require.Equal(t, tc.err == nil, handled)
		})
	}
}
