package graph

import (
	"context"
	"fmt"

	"dtm/chain"
	"dtm/db/db"
	"dtm/graph/model"
	"dtm/graph/utils"

	"github.com/google/uuid"
)

func chainReaderFromContext(ctx context.Context) (chain.Reader, error) {
	ginCtx, err := utils.GinContextFromContext(ctx)
	if err != nil {
		return nil, err
	}
	dataLoader, ok := ginCtx.Value(string(db.DataLoaderKeyTripData)).(*db.TripDataLoader)
	if !ok {
		return nil, fmt.Errorf("data loader is not available")
	}
	return db.NewRecordChainReader(dataLoader), nil
}

func resetTripDataLoader(ctx context.Context) {
	ginCtx, err := utils.GinContextFromContext(ctx)
	if err != nil {
		return
	}
	if dataLoader, ok := ginCtx.Value(string(db.DataLoaderKeyTripData)).(*db.TripDataLoader); ok {
		dataLoader.Reset()
	}
}

func (r *Resolver) recordFactory(ctx context.Context) (*chain.RecordFactory, chain.Reader, error) {
	reader, err := chainReaderFromContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	return chain.NewRecordFactory(r.recordChainStore(), reader), reader, nil
}

func (r *tripResolver) calculateMoneyShare(ctx context.Context, trip *model.Trip) (chain.MoneyShareResult, error) {
	_, reader, err := r.recordFactory(ctx)
	if err != nil {
		return chain.MoneyShareResult{}, err
	}
	tripID, err := uuid.Parse(trip.ID)
	if err != nil {
		return chain.MoneyShareResult{}, fmt.Errorf("invalid trip ID: %w", err)
	}
	return chain.NewTrip(tripID, r.recordChainStore(), reader).CalculateMoneyShare(ctx)
}
