package graph

import (
	"context"

	"dtm/chain"
	"dtm/db/db"
	"dtm/mq/mq"

	"github.com/google/uuid"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.
// ex: put your database connection or HTTP client in here.

type Resolver struct {
	TripDB                  db.TripDBWrapper
	ChainStore              chain.Store
	TripMessageQueueWrapper mq.TripMessageQueueWrapper
}

func (r *Resolver) recordChainStore() chain.Store {
	if r.ChainStore != nil {
		return r.ChainStore
	}
	return r.TripDB
}

func (r *Resolver) chainReader(ctx context.Context) (chain.Reader, error) {
	loader, err := db.TripDataLoaderFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return loader, nil
}

func (r *Resolver) recordFactory(ctx context.Context) (*chain.RecordFactory, error) {
	reader, err := r.chainReader(ctx)
	if err != nil {
		return nil, err
	}
	return chain.NewRecordFactory(r.recordChainStore(), reader), nil
}

func (r *Resolver) newTrip(ctx context.Context, tripID uuid.UUID) (*chain.Trip, error) {
	reader, err := r.chainReader(ctx)
	if err != nil {
		return nil, err
	}
	return chain.NewTrip(tripID, r.recordChainStore(), reader), nil
}
