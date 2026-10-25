package auth

import (
	"context"
	"dtm/domain"
	"github.com/google/uuid"
)

func (s *Service) SubscribedTrips(ctx context.Context, userID uuid.UUID) ([]domain.SubscribedTrip, error) {
	return s.Store.SubscribedTrips(ctx, userID)
}
func (s *Service) SubscribeTrip(ctx context.Context, userID, tripID uuid.UUID) error {
	return s.Store.SubscribeTrip(ctx, userID, tripID)
}
func (s *Service) UnsubscribeTrip(ctx context.Context, userID, tripID uuid.UUID) error {
	return s.Store.UnsubscribeTrip(ctx, userID, tripID)
}

func (s *Service) TouchSubscribedTrip(ctx context.Context, userID, tripID uuid.UUID) error {
	return s.Store.TouchSubscribedTrip(ctx, userID, tripID)
}
