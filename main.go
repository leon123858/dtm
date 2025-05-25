package main

import "dtm/tx"

func main() {
	// user input
	userPaymentList := []tx.UserPayment{
		{
			Name:             "KTV",
			Amount:           2334,
			PrePayAddress:    "陳",
			ShouldPayAddress: []string{"陳", "蔡", "佑", "謝", "盧"},
		},
		{
			Name:             "alcohol",
			Amount:           750,
			PrePayAddress:    "陳",
			ShouldPayAddress: []string{"陳", "佑", "盧"},
		},
		{
			Name:             "cookie",
			Amount:           139,
			PrePayAddress:    "陳",
			ShouldPayAddress: []string{"蔡"},
		},
		{
			Name:             "milk",
			Amount:           117,
			PrePayAddress:    "謝",
			ShouldPayAddress: []string{"蔡"},
		},
		{
			Name:             "Game",
			Amount:           3500,
			PrePayAddress:    "佑",
			ShouldPayAddress: []string{"陳", "蔡", "佑", "謝", "盧", "朋"},
		},
		{
			Name:             "Dinner",
			Amount:           1900,
			PrePayAddress:    "盧",
			ShouldPayAddress: []string{"陳", "蔡", "佑", "謝", "盧", "朋"},
		},
		{
			Name:             "Taxi100",
			Amount:           100,
			PrePayAddress:    "蔡",
			ShouldPayAddress: []string{"陳", "蔡", "盧", "朋"},
		},
		{
			Name:             "Taxi260",
			Amount:           260,
			PrePayAddress:    "謝",
			ShouldPayAddress: []string{"陳", "佑", "謝", "朋"},
		},
	}
	// Convert UserPayment to Tx using the AverageSplitStrategy
	txList := make([]tx.Tx, 0, len(userPaymentList))
	for _, up := range userPaymentList {
		tx, err := up.ToTx(tx.AverageSplitStrategy)
		if err != nil {
			panic(err) // Handle error appropriately in production code
		}
		txList = append(txList, tx)
	}
	// Create a TxPackage from the generated transactions
	txPackage := tx.TxPackage{
		Name:   "UserPaymentsPackage",
		TxList: txList,
	}

	// Process the transactions to get the cash flow for each address
	cashList := txPackage.ProcessTransactions()
	// Print the cash flow for each address
	println("Initial Cash List:")
	tx.PrintCash(cashList)
	// Normalize the cash
	println("Normalized Cash List:")
	cashList = tx.NormalizeCash(cashList)
	tx.PrintCash(cashList)
	// Convert the cash list to a TxPackage
	txPackageFromCash, diff, err := tx.CashListToTxPackage(cashList, "5_25劇本殺", tx.TxListGenerateWithMixMap)
	if err != nil {
		panic(err) // Handle error appropriately in production code
	}
	println("less input money: %.0f", diff)
	println("final payment:")
	println(txPackageFromCash.String())
}
