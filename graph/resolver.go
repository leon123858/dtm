package graph

import (
	"context"
	"dtm/domain"
	"dtm/graph/model"
	"dtm/graph/utils"
	"dtm/services/auth"

	"fmt"
	"strconv"

	"dtm/adapters/mq/mq"
	tripservice "dtm/services/trip"

	"github.com/google/uuid"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.
// ex: put your database connection or HTTP client in here.

type Resolver struct {
	Auth                    auth.Service
	RecordFactory           tripservice.RecordFactory
	TripFactory             tripservice.TripFactory
	TripMessageQueueWrapper mq.TripMessageQueueWrapper
}

func (r *Resolver) recordFactory(ctx context.Context) (tripservice.RecordFactory, error) {
	if r.RecordFactory == nil {
		return nil, fmt.Errorf("record factory is not configured")
	}
	return r.RecordFactory, nil
}

func (r *Resolver) tripForID(tripID uuid.UUID, options ...tripservice.ReadOptions) (tripservice.Trip, error) {
	if r.TripFactory == nil {
		return nil, fmt.Errorf("trip factory is not configured")
	}
	return r.TripFactory.ForTrip(tripID, options...), nil
}

func (r *Resolver) userModel(ctx context.Context, userID uuid.UUID) (*model.User, error) {
	if r.Auth == nil {
		return nil, utils.UserError("INTERNAL_SERVER_ERROR")
	}
	trips, err := r.Auth.SubscribedTrips(ctx, userID)
	if err != nil {
		return nil, utils.MemberError(err)
	}
	out := &model.User{ID: userID.String(), SubscribedTrips: []*model.SubscribedTrip{}}
	for _, trip := range trips {
		out.SubscribedTrips = append(out.SubscribedTrips, &model.SubscribedTrip{ID: trip.ID.String(), Name: trip.Name, LastViewedAt: strconv.FormatInt(trip.LastViewedAt.UnixMilli(), 10)})
	}
	return out, nil
}

func (r *Resolver) changeSubscription(ctx context.Context, tripID string, subscribe bool) (*model.User, error) {
	userID, ok := domain.UserIDFromContext(ctx)
	if !ok {
		return nil, utils.UserError("UNAUTHENTICATED")
	}
	id, err := uuid.Parse(tripID)
	if err != nil {
		return nil, utils.UserError("BAD_USER_INPUT")
	}
	if r.Auth == nil {
		return nil, utils.UserError("INTERNAL_SERVER_ERROR")
	}
	if subscribe {
		err = r.Auth.SubscribeTrip(ctx, userID, id)
	} else {
		err = r.Auth.UnsubscribeTrip(ctx, userID, id)
	}
	if err != nil {
		return nil, utils.MemberError(err)
	}
	return r.userModel(ctx, userID)
}
