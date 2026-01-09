package utils

import (
	"context"
	"dtm/graph/model"
	"dtm/tx"
	"fmt"

	"dtm/db/db"

	"github.com/google/uuid"
)

type CalculateMoneyShareResult struct {
	txPackage      *tx.Package
	totalRemaining float64
	err            error
	isValid        bool
}

type tripMoneyShareKeyType struct {
	key string
}

var TripMoneyShareKey = tripMoneyShareKeyType{
	key: "trip_money_share",
}

func CalculateMoneyShare(ctx context.Context, obj *model.Trip) (*tx.Package, float64, bool, error) {
	if result, ok := ctx.Value(TripMoneyShareKey).(CalculateMoneyShareResult); ok {
		return result.txPackage, result.totalRemaining, result.isValid, result.err
	}

	ginCtx, err := GinContextFromContext(ctx)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get Gin context: %w", err)
	}
	dataLoader, ok := ginCtx.Value(string(db.DataLoaderKeyTripData)).(*db.TripDataLoader)
	if !ok {
		return nil, 0, false, fmt.Errorf("data loader is not available")
	}
	tripID, err := uuid.Parse(obj.ID)
	if err != nil {
		return nil, 0, false, fmt.Errorf("invalid trip ID: %w", err)
	}
	records, err := dataLoader.GetRecordInfoList.Load(ctx, tripID)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get records for trip %s: %w", tripID, err)
	}

	recordAddresses := make([][]db.ExtendAddress, len(records))
	for i, record := range records {
		recordAddresses[i], err = dataLoader.GetRecordShouldPayList.Load(ctx, record.ID)
		if err != nil {
			err = fmt.Errorf("failed to get should pay addresses for record %s: %w", record.ID, err)
			ctx = context.WithValue(ctx, TripMoneyShareKey, CalculateMoneyShareResult{err: err})
			if ctx == nil {
				return nil, 0, false, err
			}
			return nil, 0, false, err
		}
	}

	payments := make([]tx.UserPayment, 0, len(records))
	for i, record := range records {
		if record.Amount <= 0 {
			continue
		}
		payment := tx.UserPayment{
			Name:             record.Name,
			Amount:           record.Amount,
			PrePayAddress:    string(record.PrePayAddress),
			ShouldPayAddress: make([]string, len(recordAddresses[i])),
			ExtendPayMsg:     make([]float64, len(recordAddresses[i])),
			PaymentType:      int(record.Category),
		}
		for j, addr := range recordAddresses[i] {
			payment.ShouldPayAddress[j] = string(addr.Address)
			payment.ExtendPayMsg[j] = addr.ExtendMsg
		}
		payments = append(payments, payment)
	}

	txPackage, totalRemaining, err := tx.ShareMoneyEasy(payments)
	if err == nil {
		ctx = context.WithValue(ctx, TripMoneyShareKey, CalculateMoneyShareResult{
			txPackage:      &txPackage,
			totalRemaining: totalRemaining,
			err:            nil,
			isValid:        true,
		})
		if ctx == nil {
			return &txPackage, totalRemaining, true, fmt.Errorf("context is nil after setting trip money share result")
		}
		return &txPackage, totalRemaining, true, nil
	}

	ctx = context.WithValue(ctx, TripMoneyShareKey, CalculateMoneyShareResult{
		txPackage:      nil,
		totalRemaining: 0,
		err:            nil,
		isValid:        false,
	})
	if ctx == nil {
		return &txPackage, totalRemaining, false, fmt.Errorf("context is nil after setting trip money share result")
	}
	// println("Error in ShareMoneyEasy:", err)
	return nil, 0, false, nil
}

func GetShouldPayList(ctx context.Context, obj *model.Record) ([]db.ExtendAddress, error) {
	ginCtx, err := GinContextFromContext(ctx)
	if err != nil {
		return nil, err
	}
	dataLoader, ok := ginCtx.Value(string(db.DataLoaderKeyTripData)).(*db.TripDataLoader)
	if !ok {
		return nil, fmt.Errorf("data loader is not available")
	}

	recordID, err := uuid.Parse(obj.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid record ID: %w", err)
	}

	// DataLoader may handle batch and cache
	addresses, err := dataLoader.GetRecordShouldPayList.Load(ctx, recordID)
	if err != nil {
		return nil, fmt.Errorf("failed to get should pay addresses: %w", err)
	}

	return addresses, nil
}
