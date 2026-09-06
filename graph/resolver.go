package graph

import (
	"context"
	"fmt"

	"dtm/adapters/mq/mq"
	tripservice "dtm/services/trip"

	"github.com/google/uuid"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.
// ex: put your database connection or HTTP client in here.

type Resolver struct {
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
