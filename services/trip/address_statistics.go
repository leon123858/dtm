package trip

import (
	"context"
	"errors"
	"fmt"

	"dtm/services/tx"
	"github.com/google/uuid"
)

// CalculateAddressStatistics skips invalid records entirely, including their record counts.
// Historical versions and deleted tails never contribute to current totals.
func (t *trip) CalculateAddressStatistics(ctx context.Context) (map[uuid.UUID]AddressStatistics, error) {
	projection, err := t.loadProjection(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]AddressStatistics)
	for _, tail := range projection.tails {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := t.policy.Validate(tail.Record); err != nil {
			if errors.Is(err, ErrInvalidRecordSnapshot) {
				continue
			}
			return nil, err
		}
		payment := paymentFromRecord(tail.RecordInfo, tail.ShouldPayAddress)
		transaction, err := payment.ToTx(tx.ShareMoneyStrategyFactory(payment.PaymentType))
		if err != nil {
			return nil, fmt.Errorf("split record %s: %w", tail.ID, err)
		}
		payerID := transaction.Output.Address.ID
		payer := result[payerID]
		payer.TotalPrepaid += transaction.Output.Amount
		payer.ActiveRecordCount++
		result[payerID] = payer
		// Validation guarantees unique should-pay addresses. The payer was counted above.
		for _, share := range transaction.Input {
			id := share.Address.ID
			statistics := result[id]
			statistics.TotalExpense += share.Amount
			if id != payerID {
				statistics.ActiveRecordCount++
			}
			result[id] = statistics
		}
	}
	return result, nil
}
