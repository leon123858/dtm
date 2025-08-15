package tx

import "fmt"

func AverageSplitStrategy(up *UserPayment) (Tx, error) {
	// first check
	if len(up.ShouldPayAddress) == 0 {
		return Tx{}, fmt.Errorf("UserPayment '%s' must have at least one ShouldPayAddress for AverageSplitStrategy", up.Name)
	}

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

func FixMoneySplitStrategy(up *UserPayment) (Tx, error) {
	// first check
	if len(up.ShouldPayAddress) == 0 {
		return Tx{}, fmt.Errorf("UserPayment '%s' must have at least one ShouldPayAddress for AverageSplitStrategy", up.Name)
	}
	if len(up.ExtendPayMsg) != len(up.ShouldPayAddress) {
		return Tx{}, fmt.Errorf("UserPayment '%s' ExtendPayMsg must have the same length as ShouldPayAddress for AverageSplitStrategy", up.Name)
	}
	for _, u := range up.ExtendPayMsg {
		if u < 0 {
			return Tx{}, fmt.Errorf("UserPayment '%s' ExtendPayMsg must be non-negative", up.Name)
		}
	}

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
	for i, u := range up.ShouldPayAddress {
		tx.Input = append(tx.Input, Payment{
			Amount:  up.ExtendPayMsg[i],
			Address: u,
		})
	}

	return tx, nil
}

func PartMoneySplitStrategy(up *UserPayment) (Tx, error) {
	// first check
	if len(up.ShouldPayAddress) == 0 {
		return Tx{}, fmt.Errorf("UserPayment '%s' must have at least one ShouldPayAddress for PartMoneySplitStrategy", up.Name)
	}
	if len(up.ExtendPayMsg) != len(up.ShouldPayAddress) {
		return Tx{}, fmt.Errorf("UserPayment '%s' ExtendPayMsg must have the same length as ShouldPayAddress for PartMoneySplitStrategy", up.Name)
	}
	for _, u := range up.ExtendPayMsg {
		if u < 0 {
			return Tx{}, fmt.Errorf("UserPayment '%s' ExtendPayMsg must be non-negative", up.Name)
		}
	}

	// Create the transaction
	tx := Tx{
		Name:  up.Name,
		Input: []Payment{},
		Output: Payment{
			Amount:  up.Amount,
			Address: up.PrePayAddress,
		},
	}

	sumOfPart := 0.0
	for _, u := range up.ExtendPayMsg {
		sumOfPart += u
	}

	// should pay user split output as input
	for i, u := range up.ShouldPayAddress {
		tx.Input = append(tx.Input, Payment{
			Amount:  up.Amount * (up.ExtendPayMsg[i] / sumOfPart),
			Address: u,
		})
	}

	return tx, nil
}

func ShareMoneyStrategyFactory(strategyEnum int) UserPaymentToTxStrategy {
	switch strategyEnum {
	case 0:
		return AverageSplitStrategy
	case 1:
		return FixMoneySplitStrategy
	case 2:
		return PartMoneySplitStrategy
	default:
		return nil
	}
}

func (up *UserPayment) ToTx(strategy UserPaymentToTxStrategy) (Tx, error) {
	if strategy == nil {
		return Tx{}, fmt.Errorf("conversion strategy cannot be nil")
	}

	if up.PrePayAddress == "" {
		return Tx{}, fmt.Errorf("UserPayment '%s' must have a PrePayAddress", up.Name)
	}
	if up.Amount <= 0 {
		return Tx{}, fmt.Errorf("UserPayment '%s' amount must be positive", up.Name)
	}

	return strategy(up)
}
