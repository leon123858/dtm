package tx

import (
	"fmt"
	"math"
)

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
	if sumOfPart <= 0 {
		return Tx{}, fmt.Errorf("ExtendPayMsg must have a positive sum")
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

func FixBeforeAverageMoneySplitStrategy(up *UserPayment) (Tx, error) {
	// first check
	if len(up.ShouldPayAddress) == 0 {
		return Tx{}, fmt.Errorf("UserPayment '%s' must have at least one ShouldPayAddress for AverageSplitStrategy", up.Name)
	}
	if len(up.ExtendPayMsg) != len(up.ShouldPayAddress) {
		return Tx{}, fmt.Errorf("UserPayment '%s' ExtendPayMsg must have the same length as ShouldPayAddress for AverageSplitStrategy", up.Name)
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

	lastMoney := up.Amount
	countOfAverage := len(up.ShouldPayAddress)
	for _, u := range up.ExtendPayMsg {
		lastMoney -= math.Abs(u)
		if u < 0 {
			countOfAverage--
		}
	}
	if lastMoney < 0 {
		return Tx{}, fmt.Errorf("after deducting fixed amounts, the remaining amount to be split is negative")
	}

	var averageMoney float64 = 0
	if countOfAverage > 0 {
		averageMoney = lastMoney / float64(countOfAverage)
	}
	if math.IsNaN(averageMoney) {
		return Tx{}, fmt.Errorf("averageMoney is NaN, please check the input")
	}

	for i, u := range up.ExtendPayMsg {
		if u < 0 {
			tx.Input = append(tx.Input, Payment{
				Amount:  -u,
				Address: up.ShouldPayAddress[i],
			})
		} else {
			tx.Input = append(tx.Input, Payment{
				Amount:  averageMoney + u,
				Address: up.ShouldPayAddress[i],
			})
		}
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
	case 3:
		return FixBeforeAverageMoneySplitStrategy
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
