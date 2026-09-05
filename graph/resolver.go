package graph

import (
	"context"
	"fmt"

	"dtm/chain"
	"dtm/mq/mq"

	"github.com/google/uuid"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.
// ex: put your database connection or HTTP client in here.

type Resolver struct {
	RecordFactory           chain.RecordFactory
	TripFactory             chain.TripFactory
	TripMessageQueueWrapper mq.TripMessageQueueWrapper
}

func (r *Resolver) recordFactory(ctx context.Context) (chain.RecordFactory, error) {
	if r.RecordFactory == nil {
		return nil, fmt.Errorf("record factory is not configured")
	}
	return r.RecordFactory, nil
}

func (r *Resolver) tripForID(tripID uuid.UUID) (chain.Trip, error) {
	if r.TripFactory == nil {
		return nil, fmt.Errorf("trip factory is not configured")
	}
	return r.TripFactory.ForTrip(tripID), nil
}
