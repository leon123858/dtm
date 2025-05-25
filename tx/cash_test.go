package tx

import (
	"container/list"
	"fmt"
	"math"
	"sort"
	"testing"
)

func floatEquals(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

// Custom DeepEqual for Cash slice, considering float precision and order
func cashListEquals(t *testing.T, got, want []Cash, msg string) {
	if len(got) != len(want) {
		t.Errorf("%s: lengths differ, got %d, want %d", msg, len(got), len(want))
		return
	}

	// Sort both slices by address for consistent comparison
	sort.Slice(got, func(i, j int) bool { return got[i].Address < got[j].Address })
	sort.Slice(want, func(i, j int) bool { return want[i].Address < want[j].Address })

	for i := range got {
		if got[i].Address != want[i].Address {
			t.Errorf("%s: Addresses at index %d differ, got %s, want %s", msg, i, got[i].Address, want[i].Address)
		}
		if !floatEquals(got[i].InputAmount, want[i].InputAmount) {
			t.Errorf("%s: InputAmount for %s at index %d differs, got %f, want %f", msg, got[i].Address, i, got[i].InputAmount, want[i].InputAmount)
		}
		if !floatEquals(got[i].OutputAmount, want[i].OutputAmount) {
			t.Errorf("%s: OutputAmount for %s at index %d differs, got %f, want %f", msg, got[i].Address, i, got[i].OutputAmount, want[i].OutputAmount)
		}
	}
}

// Helper to convert list.List to []Cash for easier comparison
func listToCashSlice(l *list.List) []Cash {
	if l == nil {
		return []Cash{}
	}
	slice := make([]Cash, 0, l.Len())
	for e := l.Front(); e != nil; e = e.Next() {
		slice = append(slice, e.Value.(Cash))
	}
	return slice
}

func TestNormalizeCash(t *testing.T) {
	tests := []struct {
		name     string
		cashList []Cash
		expected []Cash
	}{
		{
			name: "No duplicates, simple case",
			cashList: []Cash{
				{Address: "A", InputAmount: 10, OutputAmount: 0},
				{Address: "B", InputAmount: 0, OutputAmount: 20},
			},
			expected: []Cash{
				{Address: "A", InputAmount: 10, OutputAmount: 0},
				{Address: "B", InputAmount: 0, OutputAmount: 20},
			},
		},
		{
			name: "Duplicate addresses, aggregate amounts",
			cashList: []Cash{
				{Address: "A", InputAmount: 10, OutputAmount: 0},
				{Address: "B", InputAmount: 0, OutputAmount: 20},
				{Address: "A", InputAmount: 5, OutputAmount: 0}, // Duplicate for A
				{Address: "C", InputAmount: 15, OutputAmount: 0},
				{Address: "B", InputAmount: 0, OutputAmount: 10}, // Duplicate for B
			},
			expected: []Cash{
				{Address: "A", InputAmount: 15, OutputAmount: 0},
				{Address: "B", InputAmount: 0, OutputAmount: 30},
				{Address: "C", InputAmount: 15, OutputAmount: 0},
			},
		},
		{
			name:     "Empty cash list",
			cashList: []Cash{},
			expected: []Cash{},
		},
		{
			name: "Addresses with both input and output (should aggregate correctly)",
			cashList: []Cash{
				{Address: "X", InputAmount: 10, OutputAmount: 5},
				{Address: "Y", InputAmount: 20, OutputAmount: 10},
				{Address: "X", InputAmount: 5, OutputAmount: 8},
			},
			expected: []Cash{
				{Address: "X", InputAmount: 2, OutputAmount: 0},
				{Address: "Y", InputAmount: 10, OutputAmount: 0},
			},
		},
		{
			name: "Zero amounts",
			cashList: []Cash{
				{Address: "A", InputAmount: 0, OutputAmount: 0},
				{Address: "B", InputAmount: 10, OutputAmount: 0},
			},
			expected: []Cash{
				{Address: "A", InputAmount: 0, OutputAmount: 0},
				{Address: "B", InputAmount: 10, OutputAmount: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeCash(tt.cashList)
			cashListEquals(t, got, tt.expected, "NormalizeCash result")
		})
	}
}

func TestGenerateQueues(t *testing.T) {
	tests := []struct {
		name            string
		cashList        []Cash
		expectedInputs  []Cash // Expected sorted inputs
		expectedOutputs []Cash // Expected sorted outputs
	}{
		{
			name: "Mixed inputs and outputs, sorted correctly",
			cashList: []Cash{
				{Address: "A", InputAmount: 100, OutputAmount: 0},
				{Address: "B", InputAmount: 0, OutputAmount: 50},
				{Address: "C", InputAmount: 30, OutputAmount: 0},
				{Address: "D", InputAmount: 0, OutputAmount: 80},
				{Address: "E", InputAmount: 120, OutputAmount: 0},
			},
			expectedInputs: []Cash{
				{Address: "E", InputAmount: 120, OutputAmount: 0},
				{Address: "A", InputAmount: 100, OutputAmount: 0},
				{Address: "C", InputAmount: 30, OutputAmount: 0},
			},
			expectedOutputs: []Cash{
				{Address: "D", InputAmount: 0, OutputAmount: 80},
				{Address: "B", InputAmount: 0, OutputAmount: 50},
			},
		},
		{
			name:            "Empty cash list",
			cashList:        []Cash{},
			expectedInputs:  []Cash{},
			expectedOutputs: []Cash{},
		},
		{
			name: "Only inputs",
			cashList: []Cash{
				{Address: "X", InputAmount: 10, OutputAmount: 0},
				{Address: "Y", InputAmount: 20, OutputAmount: 0},
			},
			expectedInputs: []Cash{
				{Address: "Y", InputAmount: 20, OutputAmount: 0},
				{Address: "X", InputAmount: 10, OutputAmount: 0},
			},
			expectedOutputs: []Cash{},
		},
		{
			name: "Only outputs",
			cashList: []Cash{
				{Address: "P", InputAmount: 0, OutputAmount: 30},
				{Address: "Q", InputAmount: 0, OutputAmount: 5},
			},
			expectedInputs: []Cash{},
			expectedOutputs: []Cash{
				{Address: "P", InputAmount: 0, OutputAmount: 30},
				{Address: "Q", InputAmount: 0, OutputAmount: 5},
			},
		},
		{
			name: "Cash with both input/output (should be excluded by generateQueues logic)",
			cashList: []Cash{
				{Address: "Mixed", InputAmount: 10, OutputAmount: 5},
				{Address: "PureIn", InputAmount: 20, OutputAmount: 0},
			},
			expectedInputs: []Cash{
				{Address: "PureIn", InputAmount: 20, OutputAmount: 0},
				{Address: "Mixed", InputAmount: 10, OutputAmount: 5},
			},
			expectedOutputs: []Cash{},
		},
		{
			name: "Cash with zero or negative amounts (should be excluded)",
			cashList: []Cash{
				{Address: "Zero", InputAmount: 0, OutputAmount: 0},
				{Address: "Negative", InputAmount: -5, OutputAmount: 0},
				{Address: "Positive", InputAmount: 10, OutputAmount: 0},
			},
			expectedInputs: []Cash{
				{Address: "Positive", InputAmount: 10, OutputAmount: 0},
			},
			expectedOutputs: []Cash{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputQueue, outputQueue := generateQueues(tt.cashList)

			gotInputs := listToCashSlice(inputQueue)
			gotOutputs := listToCashSlice(outputQueue)

			// The slices coming out of listToCashSlice will already be sorted because they were pushed back
			// into the list from a sorted slice.
			// No need to sort again here unless for defensive coding.

			cashListEquals(t, gotInputs, tt.expectedInputs, "generateQueues Input Queue")
			cashListEquals(t, gotOutputs, tt.expectedOutputs, "generateQueues Output Queue")
		})
	}
}

func TestTxListGenerateWithMixMap(t *testing.T) {
	tests := []struct {
		name                   string
		initialCashList        []Cash
		expectedTxList         []Tx
		expectedRemainingInput float64
		expectingError         bool
		expectedErrorMsg       string // For specific error message matching
	}{
		{
			name: "Simple case: one input covers one output exactly",
			initialCashList: []Cash{
				{Address: "Alice", InputAmount: 100, OutputAmount: 0},
				{Address: "Bob", InputAmount: 0, OutputAmount: 100},
			},
			expectedTxList: []Tx{
				{
					Name:   "Tx_M_to_Bob",
					Input:  []Payment{{Amount: 100, Address: "Alice"}},
					Output: Payment{Amount: 100, Address: "Bob"},
				},
			},
			expectedRemainingInput: 0.0,
			expectingError:         false,
		},
		{
			name: "One input splits to cover one output",
			initialCashList: []Cash{
				{Address: "Alice", InputAmount: 100, OutputAmount: 0},
				{Address: "Bob", InputAmount: 0, OutputAmount: 70},
			},
			expectedTxList: []Tx{
				{
					Name:   "Tx_M_to_Bob",
					Input:  []Payment{{Amount: 70, Address: "Alice"}},
					Output: Payment{Amount: 70, Address: "Bob"},
				},
			},
			expectedRemainingInput: 30.0,  // 100 - 70 = 30 remains from Alice
			expectingError:         false, // This indicates remaining input as per current CashListToTxPackage logic
			expectedErrorMsg:       "",
		},
		{
			name: "Multiple inputs cover one output exactly",
			initialCashList: []Cash{
				{Address: "Alice", InputAmount: 60, OutputAmount: 0},
				{Address: "Charlie", InputAmount: 40, OutputAmount: 0},
				{Address: "Bob", InputAmount: 0, OutputAmount: 100},
			},
			expectedTxList: []Tx{
				{
					Name: "Tx_M_to_Bob", // Alice (60), Charlie (40) -> Bob (100)
					Input: []Payment{
						{Amount: 60, Address: "Alice"},
						{Amount: 40, Address: "Charlie"},
					},
					Output: Payment{Amount: 100, Address: "Bob"},
				},
			},
			expectedRemainingInput: 0.0,
			expectingError:         false,
		},
		{
			name: "Multiple inputs cover one output with split on last input",
			initialCashList: []Cash{
				{Address: "Alice", InputAmount: 60, OutputAmount: 0},
				{Address: "Charlie", InputAmount: 80, OutputAmount: 0}, // This will be split
				{Address: "Bob", InputAmount: 0, OutputAmount: 120},
			},
			expectedTxList: []Tx{
				{
					Name: "Tx_M_to_Bob", // Note: The name 'Tx_Alice_to_Bob_split' uses the first input's address
					Input: []Payment{
						{Amount: 80, Address: "Charlie"},
						{Amount: 40, Address: "Alice"}, // 120 - 80 = 40 needed from Alice
					},
					Output: Payment{Amount: 120, Address: "Bob"},
				},
			},
			expectedRemainingInput: 20.0, // 80 - 60 = 20 remains from Charlie
			expectingError:         false,
			expectedErrorMsg:       "",
		},
		{
			name: "No outputs, only inputs remaining",
			initialCashList: []Cash{
				{Address: "Alice", InputAmount: 100, OutputAmount: 0},
				{Address: "Bob", InputAmount: 20, OutputAmount: 0},
			},
			expectedTxList:         []Tx{},
			expectedRemainingInput: 120.0,
			expectingError:         false,
			expectedErrorMsg:       "",
		},
		{
			name: "Output with no inputs available",
			initialCashList: []Cash{
				{Address: "Bob", InputAmount: 0, OutputAmount: 100},
				{Address: "Charlie", InputAmount: 0, OutputAmount: 50},
			},
			expectedTxList:         []Tx{}, // No inputs, so no transactions can be formed
			expectedRemainingInput: 0.0,
			expectingError:         true, // No error from TxListGenerateWithMixMap itself, but outputs weren't covered.
		},
		{
			name: "Complex scenario: multiple outputs and inputs, some remaining",
			initialCashList: []Cash{
				{Address: "S1", InputAmount: 200, OutputAmount: 0},
				{Address: "S2", InputAmount: 50, OutputAmount: 0},
				{Address: "R1", InputAmount: 0, OutputAmount: 70},
				{Address: "R2", InputAmount: 0, OutputAmount: 120},
				{Address: "R3", InputAmount: 0, OutputAmount: 30},
			},
			expectedTxList: []Tx{
				{ // R2 (120) is largest output
					Name: "Tx_M_to_R2", // S1 (200) covers R2 (120), S1 has 80 left
					Input: []Payment{
						{Address: "S1", Amount: 120},
					},
					Output: Payment{Address: "R2", Amount: 120},
				},
				{ // R1 (70) is next largest output
					Name: "Tx_M_to_R1", // S1 (80 left) covers R1 (70), S1 has 10 left
					Input: []Payment{
						{Address: "S1", Amount: 70},
					},
					Output: Payment{Address: "R1", Amount: 70},
				},
				{ // R3 (30) is smallest output
					Name: "Tx_M_to_R3", // S1 (10 left) + S2 (50) = 60. R3 (30) covered by S1 (10) + S2 (20). S2 has 30 left.
					Input: []Payment{
						{Address: "S1", Amount: 10},
						{Address: "S2", Amount: 20},
					},
					Output: Payment{Address: "R3", Amount: 30},
				},
			},
			expectedRemainingInput: 30.0, // Remaining from S2: 50 - 20 = 30
			expectingError:         false,
			expectedErrorMsg:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Deep copy the initialCashList to avoid side effects across test runs,
			// as TxListGenerateWithMixMap modifies the passed slice.
			cashListCopy := make([]Cash, len(tt.initialCashList))
			copy(cashListCopy, tt.initialCashList)

			// The function expects pointers to slices for modification
			var gotTxList []Tx
			gotRemainingInput, err := TxListGenerateWithMixMap(&gotTxList, &cashListCopy)

			// Check for error first
			if (err != nil) != tt.expectingError {
				t.Errorf("TxListGenerateWithMixMap() error = %v, expectingError %v", err, tt.expectingError)
				return
			}
			if tt.expectingError && err != nil && tt.expectedErrorMsg != "" {
				if err.Error() != tt.expectedErrorMsg {
					t.Errorf("TxListGenerateWithMixMap() error message mismatch. Got: %q, Want: %q", err.Error(), tt.expectedErrorMsg)
				}
			}

			// Sort generated TxList by Name for consistent comparison
			sort.Slice(gotTxList, func(i, j int) bool {
				return gotTxList[i].Name < gotTxList[j].Name
			})
			sort.Slice(tt.expectedTxList, func(i, j int) bool {
				return tt.expectedTxList[i].Name < tt.expectedTxList[j].Name
			})

			// Compare generated TxList (elements by elements)
			if len(gotTxList) != len(tt.expectedTxList) {
				t.Errorf("TxListGenerateWithMixMap() generated TxList length = %v, want %v", len(gotTxList), len(tt.expectedTxList))
				return
			}
			for i := range gotTxList {
				// Compare Tx Name
				if gotTxList[i].Name != tt.expectedTxList[i].Name {
					t.Errorf("TxListGenerateWithMixMap() Tx[%d] Name got %q, want %q", i, gotTxList[i].Name, tt.expectedTxList[i].Name)
				}
				// Compare Tx Output
				if !floatEquals(gotTxList[i].Output.Amount, tt.expectedTxList[i].Output.Amount) ||
					gotTxList[i].Output.Address != tt.expectedTxList[i].Output.Address {
					t.Errorf("TxListGenerateWithMixMap() Tx[%d] Output got %v, want %v", i, gotTxList[i].Output, tt.expectedTxList[i].Output)
				}
				// Compare Tx Input (must sort to ensure order)
				sort.Slice(gotTxList[i].Input, func(j, k int) bool { return gotTxList[i].Input[j].Address < gotTxList[i].Input[k].Address })
				sort.Slice(tt.expectedTxList[i].Input, func(j, k int) bool {
					return tt.expectedTxList[i].Input[j].Address < tt.expectedTxList[i].Input[k].Address
				})

				if len(gotTxList[i].Input) != len(tt.expectedTxList[i].Input) {
					t.Errorf("TxListGenerateWithMixMap() Tx[%d] Input length got %d, want %d", i, len(gotTxList[i].Input), len(tt.expectedTxList[i].Input))
					continue
				}
				for j := range gotTxList[i].Input {
					if !floatEquals(gotTxList[i].Input[j].Amount, tt.expectedTxList[i].Input[j].Amount) ||
						gotTxList[i].Input[j].Address != tt.expectedTxList[i].Input[j].Address {
						t.Errorf("TxListGenerateWithMixMap() Tx[%d] Input[%d] got %v, want %v", i, j, gotTxList[i].Input[j], tt.expectedTxList[i].Input[j])
					}
				}
			}

			// Compare remaining input amount
			if !floatEquals(gotRemainingInput, tt.expectedRemainingInput) {
				t.Errorf("TxListGenerateWithMixMap() gotRemainingInput = %v, want %v", gotRemainingInput, tt.expectedRemainingInput)
			}
		})
	}
}

func TestCashListToTxPackage(t *testing.T) {
	// A dummy strategy that always returns specific values (success)
	successfulStrategy := func(txList *[]Tx, cashList *[]Cash) (float64, error) {
		*txList = append(*txList, Tx{Name: "DummyTx", Input: []Payment{{Amount: 10, Address: "A"}}, Output: Payment{Amount: 10, Address: "B"}})
		return 0.0, nil // No remaining input, no error
	}

	// A dummy strategy that returns an error
	errorStrategy := func(txList *[]Tx, cashList *[]Cash) (float64, error) {
		return 0.0, fmt.Errorf("strategy specific error")
	}

	// A dummy strategy that returns remaining input
	remainingInputStrategy := func(txList *[]Tx, cashList *[]Cash) (float64, error) {
		return 50.0, nil // 50.0 remaining input
	}

	tests := []struct {
		name                   string
		cashList               []Cash
		packageName            string
		strategy               TxListGenerateStrategy
		expectedTxPackageName  string
		expectedTxListCount    int
		expectedRemainingInput float64
		expectingError         bool
		expectedErrorMsg       string
	}{
		{
			name:                   "Successful strategy execution",
			cashList:               []Cash{{Address: "A", InputAmount: 10}}, // Cash list doesn't matter much for dummy strategy
			packageName:            "SuccessPack",
			strategy:               successfulStrategy,
			expectedTxPackageName:  "SuccessPack",
			expectedTxListCount:    1,
			expectedRemainingInput: 0.0,
			expectingError:         false,
		},
		{
			name:                   "Strategy returns an error",
			cashList:               []Cash{{Address: "A", InputAmount: 10}},
			packageName:            "ErrorPack",
			strategy:               errorStrategy,
			expectedTxPackageName:  "", // No package on error
			expectedTxListCount:    0,
			expectedRemainingInput: 0.0,
			expectingError:         true,
			expectedErrorMsg:       "strategy specific error",
		},
		{
			name:                   "Strategy returns remaining input (should result in error from CashListToTxPackage)",
			cashList:               []Cash{{Address: "A", InputAmount: 10}},
			packageName:            "RemainingPack",
			strategy:               remainingInputStrategy,
			expectedTxPackageName:  "", // Error due to remaining input
			expectedTxListCount:    0,  // Not inspecting TxList if error
			expectedRemainingInput: 50.0,
			expectingError:         true,
			expectedErrorMsg:       "there are remaining unspent inputs totaling 50.00",
		},
		// Test cases for real TxListGenerateWithMixMap strategy
		{
			name:                   "Real strategy: sufficient funds",
			cashList:               []Cash{{Address: "Inputter", InputAmount: 100}, {Address: "Outputter", OutputAmount: 100}},
			packageName:            "RealStrategySuccess",
			strategy:               TxListGenerateWithMixMap,
			expectedTxPackageName:  "RealStrategySuccess",
			expectedTxListCount:    1, // One Tx expected
			expectedRemainingInput: 0.0,
			expectingError:         false,
		},
		{
			name:                   "Real strategy: insufficient funds for output",
			cashList:               []Cash{{Address: "Inputter", InputAmount: 50}, {Address: "Outputter", OutputAmount: 100}},
			packageName:            "RealStrategyFailNoCover",
			strategy:               TxListGenerateWithMixMap,
			expectedTxPackageName:  "", // Error because an output couldn't be covered by available inputs
			expectedTxListCount:    0,
			expectedRemainingInput: 0.0, // The 50 from inputter would be considered remaining
			expectingError:         true,
			expectedErrorMsg:       "unexpected condition: collected inputs sum 50.00 is less than output 100.00 for Outputter", // This is the error from TxListGenerateWithMixMap
		},
		{
			name:                   "Real strategy: with remaining input",
			cashList:               []Cash{{Address: "Inputter", InputAmount: 100}, {Address: "Outputter", OutputAmount: 50}},
			packageName:            "RealStrategyRemaining",
			strategy:               TxListGenerateWithMixMap,
			expectedTxPackageName:  "",   // Expecting error from CashListToTxPackage due to remaining input
			expectedTxListCount:    1,    // One Tx would be generated by strategy
			expectedRemainingInput: 50.0, // 100 - 50 = 50 remaining
			expectingError:         true,
			expectedErrorMsg:       "there are remaining unspent inputs totaling 50.00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Deep copy the cashList as strategy might modify it
			cashListCopy := make([]Cash, len(tt.cashList))
			copy(cashListCopy, tt.cashList)

			gotTxPackage, gotRemainingInput, err := CashListToTxPackage(cashListCopy, tt.packageName, tt.strategy)

			// Check for error first
			if (err != nil) != tt.expectingError {
				t.Errorf("CashListToTxPackage() error = %v, expectingError %v", err, tt.expectingError)
				return
			}
			if tt.expectingError && err != nil && tt.expectedErrorMsg != "" {
				if err.Error() != tt.expectedErrorMsg {
					t.Errorf("CashListToTxPackage() error message mismatch. Got: %q, Want: %q", err.Error(), tt.expectedErrorMsg)
				}
			}

			// If no error expected, compare the returned TxPackage and remaining input
			if !tt.expectingError { // Only compare on success
				if gotTxPackage.Name != tt.expectedTxPackageName {
					t.Errorf("CashListToTxPackage() gotTxPackage.Name = %q, want %q", gotTxPackage.Name, tt.expectedTxPackageName)
				}
				if len(gotTxPackage.TxList) != tt.expectedTxListCount {
					t.Errorf("CashListToTxPackage() gotTxPackage.TxList count = %d, want %d", len(gotTxPackage.TxList), tt.expectedTxListCount)
				}
			}

			// Compare remaining input amount (even if error, it's a return value)
			if !floatEquals(gotRemainingInput, tt.expectedRemainingInput) {
				t.Errorf("CashListToTxPackage() gotRemainingInput = %v, want %v", gotRemainingInput, tt.expectedRemainingInput)
			}
		})
	}
}
