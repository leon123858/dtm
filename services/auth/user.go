package auth

import (
	"context"
	"dtm/domain"
	"github.com/google/uuid"
)

func (s *service) SubscribedTrips(ctx context.Context, userID uuid.UUID) ([]domain.SubscribedTrip, error) {
	return s.store.SubscribedTrips(ctx, userID)
}
func (s *service) SubscribeTrip(ctx context.Context, userID, tripID uuid.UUID) error {
	return s.store.SubscribeTrip(ctx, userID, tripID)
}
func (s *service) UnsubscribeTrip(ctx context.Context, userID, tripID uuid.UUID) error {
	return s.store.UnsubscribeTrip(ctx, userID, tripID)
}

func (s *service) TouchSubscribedTrip(ctx context.Context, userID, tripID uuid.UUID) error {
	return s.store.TouchSubscribedTrip(ctx, userID, tripID)
}

func (s *service) GetUser(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return s.store.GetUser(ctx, id)
}
func (s *service) UpdateMe(ctx context.Context, name *string, sub *bool) (domain.User, error) {
	id, ok := domain.UserIDFromContext(ctx)
	if !ok {
		return domain.User{}, domain.ErrUnauthenticated
	}
	return s.store.UpdateUser(ctx, id, name, sub)
}
