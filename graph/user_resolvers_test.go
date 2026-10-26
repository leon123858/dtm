package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dtm/domain"
	"dtm/services/auth"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type userFieldAuth struct {
	auth.Service
	userID  uuid.UUID
	trips   []domain.SubscribedTrip
	reads   int
	writes  []string
	readErr error
}

func (s *userFieldAuth) SubscribedTrips(_ context.Context, id uuid.UUID) ([]domain.SubscribedTrip, error) {
	if id != s.userID {
		return nil, errors.New("wrong user")
	}
	s.reads++
	return s.trips, s.readErr
}
func (s *userFieldAuth) SubscribeTrip(_ context.Context, userID, tripID uuid.UUID) error {
	if userID != s.userID {
		return errors.New("wrong user")
	}
	s.writes = append(s.writes, "subscribeTrip")
	s.trips = []domain.SubscribedTrip{{ID: tripID, Name: "saved", LastViewedAt: time.UnixMilli(1234)}}
	return nil
}
func (s *userFieldAuth) UnsubscribeTrip(_ context.Context, userID, _ uuid.UUID) error {
	if userID != s.userID {
		return errors.New("wrong user")
	}
	s.writes = append(s.writes, "unsubscribeTrip")
	s.trips = nil
	return nil
}
func (s *userFieldAuth) TouchSubscribedTrip(_ context.Context, userID, tripID uuid.UUID) error {
	if userID != s.userID {
		return errors.New("wrong user")
	}
	s.writes = append(s.writes, "touchSubscribedTrip")
	s.trips = []domain.SubscribedTrip{{ID: tripID, Name: "touched", LastViewedAt: time.UnixMilli(5678)}}
	return nil
}

func userFieldRequest(t *testing.T, service *userFieldAuth, authenticated bool, query string) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/query", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if authenticated {
		req = req.WithContext(domain.WithUserID(req.Context(), service.userID))
	}
	recorder := httptest.NewRecorder()
	handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: &Resolver{Auth: service}})).ServeHTTP(recorder, req)
	var result map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	return result
}

func TestUserFieldsLoadOnlyWhenSelected(t *testing.T) {
	tripID := uuid.NewString()
	for _, operation := range []string{"me", "subscribeTrip", "unsubscribeTrip", "touchSubscribedTrip"} {
		t.Run(operation, func(t *testing.T) {
			for _, selection := range []string{"id", "id subscribedTrips @skip(if: true) { id }", "id subscribedTrips @include(if: false) { id }", "id ... on User @skip(if: true) { subscribedTrips { id } }", "id ...Details"} {
				service := &userFieldAuth{userID: uuid.New()}
				query := fmt.Sprintf("{ me { %s } }", selection)
				if operation != "me" {
					query = fmt.Sprintf("mutation { %s(tripId:%q) { %s } }", operation, tripID, selection)
				}
				selected := selection == "id ...Details"
				if selected {
					query += " fragment Details on User { subscribedTrips { id name lastViewedAt } }"
				}
				result := userFieldRequest(t, service, true, query)
				require.Nil(t, result["errors"])
				user := result["data"].(map[string]any)[operation].(map[string]any)
				require.Equal(t, service.userID.String(), user["id"])
				if selected {
					require.Equal(t, 1, service.reads)
					trips := user["subscribedTrips"].([]any)
					if operation == "me" || operation == "unsubscribeTrip" {
						require.Empty(t, trips)
					} else {
						require.Len(t, trips, 1)
						expectedName, expectedTime := "saved", "1234"
						if operation == "touchSubscribedTrip" {
							expectedName, expectedTime = "touched", "5678"
						}
						require.Equal(t, map[string]any{"id": tripID, "name": expectedName, "lastViewedAt": expectedTime}, trips[0])
					}
				} else {
					require.Zero(t, service.reads)
				}
				if operation != "me" {
					require.Equal(t, []string{operation}, service.writes)
				}
			}
		})
	}
}

func TestUserFieldReadFailureAndAuthentication(t *testing.T) {
	service := &userFieldAuth{userID: uuid.New(), readErr: errors.New("storage")}
	result := userFieldRequest(t, service, true, "{ me { id } }")
	require.Nil(t, result["errors"])
	require.Zero(t, service.reads)
	result = userFieldRequest(t, service, true, "{ me { subscribedTrips { id } } }")
	require.Nil(t, result["data"].(map[string]any)["me"])
	gqlErr := result["errors"].([]any)[0].(map[string]any)
	require.Equal(t, []any{"me", "subscribedTrips"}, gqlErr["path"])
	require.Equal(t, "INTERNAL_SERVER_ERROR", gqlErr["extensions"].(map[string]any)["code"])
	result = userFieldRequest(t, service, false, "{ me { subscribedTrips { id } } }")
	require.Nil(t, result["errors"])
	require.Nil(t, result["data"].(map[string]any)["me"])
	require.Equal(t, 1, service.reads)
	result = userFieldRequest(t, service, true, fmt.Sprintf("mutation { subscribeTrip(tripId:%q) { subscribedTrips { id } } }", uuid.NewString()))
	require.Nil(t, result["data"])
	gqlErr = result["errors"].([]any)[0].(map[string]any)
	require.Equal(t, []any{"subscribeTrip", "subscribedTrips"}, gqlErr["path"])
	require.Equal(t, []string{"subscribeTrip"}, service.writes)
}
