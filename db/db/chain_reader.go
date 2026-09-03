package db

import (
	"context"

	"dtm/chain"
	"dtm/domain"

	"github.com/google/uuid"
)

type recordChainReader struct {
	loader *TripDataLoader
}

// NewRecordChainReader adapts a request-scoped TripDataLoader to the chain
// service's read contract.
func NewRecordChainReader(loader *TripDataLoader) chain.Reader {
	return &recordChainReader{loader: loader}
}

func (r *recordChainReader) LoadTripRecords(ctx context.Context, tripID uuid.UUID) ([]domain.RecordInfo, error) {
	return r.loader.GetRecordInfoList.Load(ctx, tripID)
}

func (r *recordChainReader) LoadRecord(ctx context.Context, recordID uuid.UUID) (chain.RecordNode, error) {
	return r.loader.GetRecord.Load(ctx, recordID)
}

func (r *recordChainReader) InvalidateRecords(recordIDs ...uuid.UUID) {
	for _, recordID := range recordIDs {
		r.loader.GetRecord.Clear(recordID)
	}
}

func (r *recordChainReader) LoadTripAddresses(ctx context.Context, tripID uuid.UUID) ([]domain.Address, error) {
	return r.loader.GetTripAddressList.Load(ctx, tripID)
}

func (r *recordChainReader) LoadRecordShouldPay(ctx context.Context, recordID uuid.UUID) ([]domain.ExtendAddress, error) {
	return r.loader.GetRecordShouldPayList.Load(ctx, recordID)
}
