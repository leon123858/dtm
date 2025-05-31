package loader

import (
	"context"
	"dtm/db/db"

	"github.com/google/uuid"
)

func FetchTripRecordList(ctx context.Context, recordId []uuid.UUID) (ret []db.Record, errs []error) {
	return
}

func FetchTripAddressList(ctx context.Context, tripId []uuid.UUID) (ret []db.Address, errs []error) {
	return
}
