package tx

import (
	"fmt"
	"math"
)

// Validate calculates the total amount of inputs and outputs,
// It returns the total input amount, total output amount
func (t *Tx) Validate() (float64, float64) {
	totalInputAmount := 0.0
	for _, p := range t.Input {
		totalInputAmount += p.Amount
	}

	totalOutputAmount := t.Output.Amount

	return totalInputAmount, totalOutputAmount
}

func (t *Tx) BoolValidate() bool {
	totalInputAmount, totalOutputAmount := t.Validate()
	if totalInputAmount < epsilon || totalOutputAmount < epsilon {
		return false // No inputs and outputs, considered invalid
	}
	if math.Abs(totalInputAmount-totalOutputAmount) > epsilon {
		return false // Input and output amounts do not match
	}
	return true // Valid transaction
}

// ProcessTransactions calculates the total input and output amounts for each address
// within the TxList of the TxPackage, and returns a slice of Cash objects.
func (tp *TxPackage) ProcessTransactions() []Cash {
	// Use a map to aggregate amounts by address
	// The key is the address (string), and the value is a pointer to a Cash struct.
	// Using a pointer allows us to modify the struct fields directly.
	addressCashMap := make(map[string]*Cash)

	// Helper function to get or create a Cash entry for an address
	getCashEntry := func(addr string) *Cash {
		if entry, ok := addressCashMap[addr]; ok {
			return entry
		}
		newEntry := &Cash{Address: addr} // Create a new Cash struct
		addressCashMap[addr] = newEntry  // Store it in the map
		return newEntry
	}

	for _, tx := range tp.TxList {
		// Process Inputs (amounts leaving an address)
		for _, inputPayment := range tx.Input {
			// Get or create the Cash entry for the input address
			entry := getCashEntry(inputPayment.Address)
			entry.InputAmount += inputPayment.Amount
		}

		// Process Output (amount arriving at an address)
		// Get or create the Cash entry for the output address
		outputEntry := getCashEntry(tx.Output.Address)
		outputEntry.OutputAmount += tx.Output.Amount
	}

	// Convert the map values (pointers to Cash structs) into a slice of Cash structs
	var cashList []Cash
	for _, cashEntry := range addressCashMap {
		cashList = append(cashList, *cashEntry) // Dereference the pointer to get the actual struct
	}

	return cashList
}

func (tp *TxPackage) String() string {
	result := "TxPackage: " + tp.Name + "\n"
	for _, tx := range tp.TxList {
		result += "  Tx: " + tx.Name + "\n"
		result += "    Inputs:\n"
		for _, input := range tx.Input {
			result += "      - " + input.Address + ": " + fmt.Sprintf("%.2f", input.Amount) + "\n"
		}
		result += "    Output:\n"
		result += "      - " + tx.Output.Address + ": " + fmt.Sprintf("%.2f", tx.Output.Amount) + "\n"
	}
	return result
}

func UIList2TxList(uiList []UserPayment) ([]Tx, error) {
	txList := make([]Tx, 0, len(uiList))
	for _, up := range uiList {
		tx, err := up.ToTx(up.Strategy)
		if err != nil {
			return nil, fmt.Errorf("failed to convert UserPayment to Tx: %w", err)
		}
		if !tx.BoolValidate() {
			return nil, fmt.Errorf("invalid transaction: %s", tx.Name)
		}
		txList = append(txList, tx)
	}
	return txList, nil
}

func ShareMoneyEasyNoLog(uiList []UserPayment) (TxPackage, float64, error) {
	txList, err := UIList2TxList(uiList)
	if err != nil {
		return TxPackage{}, 0, fmt.Errorf("failed to convert UserPayment to TxList: %w", err)
	}
	// Create a TxPackage from the generated transactions
	txPackage := TxPackage{
		Name:   "UserPaymentsPackage",
		TxList: txList,
	}
	// Process the transactions to get the cash flow for each address
	cashList := txPackage.ProcessTransactions()
	// Normalize the cash
	cashList = NormalizeCash(cashList)
	// Convert the cash list to a TxPackage
	txPackageFromCash, diff, err := CashListToTxPackage(cashList, "activity", TxListGenerateWithMixMap)
	if err != nil {
		return TxPackage{}, 0, fmt.Errorf("failed to convert cash list to TxPackage: %w", err)
	}
	// println(txPackageFromCash.String())
	return txPackageFromCash, diff, nil
}

func ShareMoneyEasy(uiList []UserPayment) (TxPackage, float64, error) {
	txList, err := UIList2TxList(uiList)
	if err != nil {
		return TxPackage{}, 0, fmt.Errorf("failed to convert UserPayment to TxList: %w", err)
	}
	// Create a TxPackage from the generated transactions
	txPackage := TxPackage{
		Name:   "UserPaymentsPackage",
		TxList: txList,
	}

	// Process the transactions to get the cash flow for each address
	cashList := txPackage.ProcessTransactions()
	// Print the cash flow for each address
	println("Initial Cash List:")
	PrintCash(cashList)
	// Normalize the cash
	println("Normalized Cash List:")
	cashList = NormalizeCash(cashList)
	PrintCash(cashList)
	// Convert the cash list to a TxPackage
	txPackageFromCash, diff, err := CashListToTxPackage(cashList, "activity", TxListGenerateWithMixMap)
	if err != nil {
		return TxPackage{}, 0, fmt.Errorf("failed to convert cash list to TxPackage: %w", err)
	}
	// println(txPackageFromCash.String())
	return txPackageFromCash, diff, nil
}
