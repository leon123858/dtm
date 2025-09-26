package tx

import (
	"reflect"
	"sort"
	"testing"
)

func TestTxPackage_ProcessTransactions(t *testing.T) {
	tests := []struct {
		name             string
		txPackage        Package
		expectedCashList []Cash
	}{
		{
			name: "Single transaction, two addresses",
			txPackage: Package{
				Name: "SingleTxPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
						Output: Payment{Amount: 10.0, Address: "Bob"},
						Name:   "Tx1",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "Alice", InputAmount: 10.0, OutputAmount: 0.0},
				{Address: "Bob", InputAmount: 0.0, OutputAmount: 10.0},
			},
		},
		{
			name: "Multiple transactions, addresses interacting",
			txPackage: Package{
				Name: "ComplexTxPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 100.0, Address: "Alice"}},
						Output: Payment{Amount: 100.0, Address: "Bob"},
						Name:   "TxA",
					},
					{
						Input:  []Payment{{Amount: 50.0, Address: "Bob"}}, // Bob sends money now
						Output: Payment{Amount: 50.0, Address: "Charlie"},
						Name:   "TxB",
					},
					{
						Input:  []Payment{{Amount: 20.0, Address: "Alice"}}, // Alice sends more
						Output: Payment{Amount: 20.0, Address: "David"},
						Name:   "TxC",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "Alice", InputAmount: 120.0, OutputAmount: 0.0},  // 100 from TxA, 20 from TxC
				{Address: "Bob", InputAmount: 50.0, OutputAmount: 100.0},   // 100 to Bob (TxA), 50 from Bob (TxB)
				{Address: "Charlie", InputAmount: 0.0, OutputAmount: 50.0}, // 50 to Charlie (TxB)
				{Address: "David", InputAmount: 0.0, OutputAmount: 20.0},   // 20 to David (TxC)
			},
		},
		{
			name: "Empty transaction list",
			txPackage: Package{
				Name:   "EmptyPackage",
				TxList: []Tx{},
			},
			expectedCashList: []Cash{}, // Expect an empty list
		},
		{
			name: "Transactions with zero amounts",
			txPackage: Package{
				Name: "ZeroAmountPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 0.0, Address: "X"}},
						Output: Payment{Amount: 0.0, Address: "Y"},
						Name:   "ZeroTx",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "X", InputAmount: 0.0, OutputAmount: 0.0},
				{Address: "Y", InputAmount: 0.0, OutputAmount: 0.0},
			},
		},
		{
			name: "Address appears in both input and output within the same package",
			txPackage: Package{
				Name: "SelfTransferPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
						Output: Payment{Amount: 10.0, Address: "Bob"},
						Name:   "Tx1",
					},
					{
						Input:  []Payment{{Amount: 5.0, Address: "Bob"}},
						Output: Payment{Amount: 5.0, Address: "Alice"},
						Name:   "Tx2",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "Alice", InputAmount: 10.0, OutputAmount: 5.0}, // Input: 10 from Tx1. Output: 5 from Tx2
				{Address: "Bob", InputAmount: 5.0, OutputAmount: 10.0},   // Input: 5 from Tx2. Output: 10 from Tx1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCashList := tt.txPackage.ProcessTransactions()

			// Sort both slices to ensure consistent order for comparison
			sort.Slice(gotCashList, func(i, j int) bool {
				return gotCashList[i].Address < gotCashList[j].Address
			})
			sort.Slice(tt.expectedCashList, func(i, j int) bool {
				return tt.expectedCashList[i].Address < tt.expectedCashList[j].Address
			})

			// Check if lengths match
			if len(gotCashList) != len(tt.expectedCashList) {
				t.Errorf("ProcessTransactions() gotCashList length = %v, want %v", len(gotCashList), len(tt.expectedCashList))
				return
			}

			// Compare each Cash struct
			for i := range gotCashList {
				if !reflect.DeepEqual(gotCashList[i], tt.expectedCashList[i]) {
					t.Errorf("ProcessTransactions() at index %d: got %v, want %v", i, gotCashList[i], tt.expectedCashList[i])
				}
			}
		})
	}
}

func TestTx_Validate(t *testing.T) {
	tests := []struct {
		name           string
		tx             Tx
		expectedInput  float64
		expectedOutput float64
	}{
		{
			name: "Single input and output",
			tx: Tx{
				Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
				Output: Payment{Amount: 10.0, Address: "Bob"},
				Name:   "Tx1",
			},
			expectedInput:  10.0,
			expectedOutput: 10.0,
		},
		{
			name: "Multiple inputs, single output",
			tx: Tx{
				Input: []Payment{
					{Amount: 5.0, Address: "Alice"},
					{Amount: 15.0, Address: "Bob"},
				},
				Output: Payment{Amount: 20.0, Address: "Charlie"},
				Name:   "Tx2",
			},
			expectedInput:  20.0,
			expectedOutput: 20.0,
		},
		{
			name: "No inputs, only output",
			tx: Tx{
				Input:  []Payment{},
				Output: Payment{Amount: 10.0, Address: "Charlie"},
				Name:   "Tx3",
			},
			expectedInput:  0.0,
			expectedOutput: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, output := tt.tx.Validate()
			if input != tt.expectedInput || output != tt.expectedOutput {
				t.Errorf("Validate() = (%v, %v), want (%v, %v)", input, output, tt.expectedInput, tt.expectedOutput)
			}
		})
	}
}

func TestTxPackage_String(t *testing.T) {
	tests := []struct {
		name      string
		txPackage Package
		expected  string
	}{
		{
			name: "Single transaction package",
			txPackage: Package{
				Name: "TestPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
						Output: Payment{Amount: 10.0, Address: "Bob"},
						Name:   "Tx1",
					},
				},
			},
		},
		{
			name: "Empty transaction package",
			txPackage: Package{
				Name:   "EmptyPackage",
				TxList: []Tx{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.txPackage.String(); got == "" {
				t.Errorf("TxPackage.String() returned an empty string, expected non-empty string")
			} else {
				println("TxPackage.String() output:", got)
			}
		})
	}
}
