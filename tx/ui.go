package tx

import "fmt"

func AverageSplitStrategy(up *UserPayment) (Tx, error) {
	// Create the transaction
	tx := Tx{
		Name:  up.Name,
		Input: []Payment{},
		Output: Payment{
			Amount:  up.Amount,
			Address: up.PrePayAddress,
		},
	}

	// should pay user split output as input
	eachPayAmount := up.Amount / float64(len(up.ShouldPayAddress))
	for _, u := range up.ShouldPayAddress {
		tx.Input = append(tx.Input, Payment{
			Amount:  eachPayAmount,
			Address: u,
		})
	}

	return tx, nil
}

func (up *UserPayment) ToTx(strategy UserPaymentToTxStrategy) (Tx, error) {
	if strategy == nil {
		return Tx{}, fmt.Errorf("conversion strategy cannot be nil")
	}

	if up.PrePayAddress == "" {
		return Tx{}, fmt.Errorf("UserPayment '%s' must have a PrePayAddress", up.Name)
	}
	if len(up.ShouldPayAddress) == 0 {
		return Tx{}, fmt.Errorf("UserPayment '%s' must have at least one ShouldPayAddress for AverageSplitStrategy", up.Name)
	}
	if up.Amount <= 0 {
		return Tx{}, fmt.Errorf("UserPayment '%s' amount must be positive", up.Name)
	}

	return strategy(up)
}
