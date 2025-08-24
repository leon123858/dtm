package tx

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestAverageSplitStrategy(t *testing.T) {
	tests := []struct {
		name         string
		userPayment  *UserPayment
		expectedTx   Tx
		expectedErr  error
		expectingErr bool // Flag to indicate if an error is expected
	}{
		{
			name: "Successful conversion with positive amount and multiple recipients",
			userPayment: &UserPayment{
				Name:             "FamilyDinnerSplit",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount"},
			},
			expectedTx: Tx{
				Name: "FamilyDinnerSplit",
				Input: []Payment{
					{Amount: 50.0, Address: "BobAccount"},
					{Amount: 50.0, Address: "CharlieAccount"},
				},
				Output: Payment{Amount: 100.0, Address: "AliceAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Successful conversion with single recipient",
			userPayment: &UserPayment{
				Name:             "SinglePayment",
				Amount:           75.0,
				PrePayAddress:    "DavidAccount",
				ShouldPayAddress: []string{"EveAccount"},
			},
			expectedTx: Tx{
				Name: "SinglePayment",
				Input: []Payment{
					{Amount: 75.0, Address: "EveAccount"},
				},
				Output: Payment{Amount: 75.0, Address: "DavidAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Amount with decimal places",
			userPayment: &UserPayment{
				Name:             "DecimalSplit",
				Amount:           99.99,
				PrePayAddress:    "FrankAccount",
				ShouldPayAddress: []string{"GraceAccount", "HannahAccount", "IvanAccount"},
			},
			expectedTx: Tx{
				Name: "DecimalSplit",
				Input: []Payment{
					{Amount: 33.33, Address: "GraceAccount"},
					{Amount: 33.33, Address: "HannahAccount"},
					{Amount: 33.33, Address: "IvanAccount"},
				},
				Output: Payment{Amount: 99.99, Address: "FrankAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Include Pre Payment Address in Output",
			userPayment: &UserPayment{
				Name:             "PrePaymentTest",
				Amount:           150.0,
				PrePayAddress:    "GraceAccount",
				ShouldPayAddress: []string{"GraceAccount", "HannahAccount", "IvanAccount"},
			},
			expectedTx: Tx{
				Name: "PrePaymentTest",
				Input: []Payment{
					{Amount: 50.0, Address: "GraceAccount"},
					{Amount: 50.0, Address: "HannahAccount"},
					{Amount: 50.0, Address: "IvanAccount"},
				},
				Output: Payment{Amount: 150.0, Address: "GraceAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTx, err := AverageSplitStrategy(tt.userPayment)

			if (err != nil) != tt.expectingErr {
				t.Errorf("AverageSplitStrategy() error = %v, expectingErr %v", err, tt.expectingErr)
				return
			}
			if tt.expectingErr {
				// We don't compare the error message directly as it might vary, just that it exists.
				return
			}

			// deep compare 2 struct
			if !reflect.DeepEqual(gotTx, tt.expectedTx) {
				t.Errorf("AverageSplitStrategy() gotTx = %v, want %v", gotTx, tt.expectedTx)
			}
		})
	}
}

func TestUserPayment_ToTx(t *testing.T) {
	// A dummy strategy for successful conversions, as the actual logic is in AverageSplitStrategy
	dummyStrategy := func(up *UserPayment) (Tx, error) {
		return Tx{Name: up.Name + "_converted"}, nil
	}

	tests := []struct {
		name         string
		userPayment  *UserPayment
		strategy     UserPaymentToTxStrategy
		expectedTx   Tx
		expectedErr  error
		expectingErr bool
	}{
		{
			name: "Successful conversion with valid UserPayment and strategy",
			userPayment: &UserPayment{
				Name:             "ValidPayment",
				Amount:           100.0,
				PrePayAddress:    "Sender",
				ShouldPayAddress: []string{"Receiver"},
			},
			strategy:     dummyStrategy, // Use a dummy strategy here
			expectedTx:   Tx{Name: "ValidPayment_converted"},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Error: nil strategy provided",
			userPayment: &UserPayment{
				Name:             "NilStrategyPayment",
				Amount:           10.0,
				PrePayAddress:    "Sender",
				ShouldPayAddress: []string{"Receiver"},
			},
			strategy:     nil, // Strategy is nil
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("conversion strategy cannot be nil"),
			expectingErr: true,
		},
		{
			name: "Error: missing PrePayAddress",
			userPayment: &UserPayment{
				Name:             "MissingPrePay",
				Amount:           20.0,
				PrePayAddress:    "", // Missing PrePayAddress
				ShouldPayAddress: []string{"Receiver"},
			},
			strategy:     dummyStrategy,
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'MissingPrePay' must have a PrePayAddress"),
			expectingErr: true,
		},
		{
			name: "Error: empty ShouldPayAddress",
			userPayment: &UserPayment{
				Name:             "EmptyShouldPay",
				Amount:           30.0,
				PrePayAddress:    "Sender",
				ShouldPayAddress: []string{}, // Empty ShouldPayAddress
			},
			strategy: dummyStrategy,
			expectedTx: Tx{
				Name: "EmptyShouldPay_converted",
			},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Error: non-positive Amount (zero)",
			userPayment: &UserPayment{
				Name:             "ZeroAmount",
				Amount:           0.0, // Zero amount
				PrePayAddress:    "Sender",
				ShouldPayAddress: []string{"Receiver"},
			},
			strategy:     dummyStrategy,
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'ZeroAmount' amount must be positive"),
			expectingErr: true,
		},
		{
			name: "Error: non-positive Amount (negative)",
			userPayment: &UserPayment{
				Name:             "NegativeAmount",
				Amount:           -5.0, // Negative amount
				PrePayAddress:    "Sender",
				ShouldPayAddress: []string{"Receiver"},
			},
			strategy:     dummyStrategy,
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'NegativeAmount' amount must be positive"),
			expectingErr: true,
		},
		{
			name: "Successful conversion with avg strategy",
			userPayment: &UserPayment{
				Name:             "AvgSplitPayment",
				Amount:           120.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount"},
			},
			strategy: AverageSplitStrategy, // Use the AverageSplitStrategy directly
			expectedTx: Tx{
				Name: "AvgSplitPayment",
				Input: []Payment{
					{Amount: 60.0, Address: "BobAccount"},
					{Amount: 60.0, Address: "CharlieAccount"},
				},
				Output: Payment{Amount: 120.0, Address: "AliceAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTx, err := tt.userPayment.ToTx(tt.strategy)

			if (err != nil) != tt.expectingErr {
				t.Errorf("UserPayment.ToTx() error = %v, expectingErr %v", err, tt.expectingErr)
				return
			}

			if tt.expectingErr {
				// For error cases, we want to check if the error message contains the expected substring.
				if err != nil && tt.expectedErr != nil && err.Error() != tt.expectedErr.Error() {
					t.Errorf("UserPayment.ToTx() error message mismatch. Got: %q, Want: %q", err.Error(), tt.expectedErr.Error())
				}
				return
			}

			// deep compare 2 struct
			if !reflect.DeepEqual(gotTx, tt.expectedTx) {
				t.Errorf("AverageSplitStrategy() gotTx = %v, want %v", gotTx, tt.expectedTx)
			}
		})
	}
}

func TestFixMoneySplitStrategy(t *testing.T) {
	tests := []struct {
		name         string
		userPayment  *UserPayment
		expectedTx   Tx
		expectedErr  error
		expectingErr bool
	}{
		{
			name: "Successful conversion with fixed amounts",
			userPayment: &UserPayment{
				Name:             "FixedDinnerSplit",
				Amount:           150.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount"},
				ExtendPayMsg:     []float64{100.0, 50.0},
			},
			expectedTx: Tx{
				Name: "FixedDinnerSplit",
				Input: []Payment{
					{Amount: 100.0, Address: "BobAccount"},
					{Amount: 50.0, Address: "CharlieAccount"},
				},
				Output: Payment{Amount: 150.0, Address: "AliceAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Error: No recipients",
			userPayment: &UserPayment{
				Name:             "NoRecipients",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{},
				ExtendPayMsg:     []float64{},
			},
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'NoRecipients' must have at least one ShouldPayAddress for AverageSplitStrategy"),
			expectingErr: true,
		},
		{
			name: "Error: Mismatched lengths of ShouldPayAddress and ExtendPayMsg",
			userPayment: &UserPayment{
				Name:             "MismatchedLengths",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount"},
				ExtendPayMsg:     []float64{100.0},
			},
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'MismatchedLengths' ExtendPayMsg must have the same length as ShouldPayAddress for AverageSplitStrategy"),
			expectingErr: true,
		},
		{
			name: "Error: Negative amount in ExtendPayMsg",
			userPayment: &UserPayment{
				Name:             "NegativeAmount",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount"},
				ExtendPayMsg:     []float64{120.0, -20.0},
			},
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'NegativeAmount' ExtendPayMsg must be non-negative"),
			expectingErr: true,
		},
		{
			name: "Successful conversion where one recipient pays zero",
			userPayment: &UserPayment{
				Name:             "ZeroPaymentSplit",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount"},
				ExtendPayMsg:     []float64{100.0, 0.0},
			},
			expectedTx: Tx{
				Name: "ZeroPaymentSplit",
				Input: []Payment{
					{Amount: 100.0, Address: "BobAccount"},
					{Amount: 0.0, Address: "CharlieAccount"},
				},
				Output: Payment{Amount: 100.0, Address: "AliceAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTx, err := FixMoneySplitStrategy(tt.userPayment)

			if (err != nil) != tt.expectingErr {
				t.Errorf("FixMoneySplitStrategy() error = %v, expectingErr %v", err, tt.expectingErr)
				return
			}
			if tt.expectingErr {
				if err != nil && tt.expectedErr != nil && err.Error() != tt.expectedErr.Error() {
					t.Errorf("FixMoneySplitStrategy() error message mismatch. Got: %q, Want: %q", err.Error(), tt.expectedErr.Error())
				}
				return
			}

			if !reflect.DeepEqual(gotTx, tt.expectedTx) {
				t.Errorf("FixMoneySplitStrategy() gotTx = %v, want %v", gotTx, tt.expectedTx)
			}
		})
	}
}

func TestPartMoneySplitStrategy(t *testing.T) {
	tests := []struct {
		name         string
		userPayment  *UserPayment
		expectedTx   Tx
		expectedErr  error
		expectingErr bool
	}{
		{
			name: "Successful conversion with proportional splitting",
			userPayment: &UserPayment{
				Name:             "DinnerSplitByPortion",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount", "DavidAccount"},
				ExtendPayMsg:     []float64{1, 2, 2}, // Total parts: 5
			},
			expectedTx: Tx{
				Name: "DinnerSplitByPortion",
				Input: []Payment{
					{Amount: 20.0, Address: "BobAccount"},     // 100 * (1/5)
					{Amount: 40.0, Address: "CharlieAccount"}, // 100 * (2/5)
					{Amount: 40.0, Address: "DavidAccount"},   // 100 * (2/5)
				},
				Output: Payment{Amount: 100.0, Address: "AliceAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Error: No recipients",
			userPayment: &UserPayment{
				Name:             "NoRecipients",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{},
				ExtendPayMsg:     []float64{},
			},
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'NoRecipients' must have at least one ShouldPayAddress for PartMoneySplitStrategy"),
			expectingErr: true,
		},
		{
			name: "Error: Mismatched lengths of ShouldPayAddress and ExtendPayMsg",
			userPayment: &UserPayment{
				Name:             "MismatchedLengths",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount"},
				ExtendPayMsg:     []float64{1, 2},
			},
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'MismatchedLengths' ExtendPayMsg must have the same length as ShouldPayAddress for PartMoneySplitStrategy"),
			expectingErr: true,
		},
		{
			name: "Error: Negative part in ExtendPayMsg",
			userPayment: &UserPayment{
				Name:             "NegativePart",
				Amount:           100.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount"},
				ExtendPayMsg:     []float64{1, -1},
			},
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'NegativePart' ExtendPayMsg must be non-negative"),
			expectingErr: true,
		},
		{
			name: "Successful split with one part being zero",
			userPayment: &UserPayment{
				Name:             "ZeroPartSplit",
				Amount:           120.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount", "DavidAccount"},
				ExtendPayMsg:     []float64{3, 0, 1}, // Total parts: 4
			},
			expectedTx: Tx{
				Name: "ZeroPartSplit",
				Input: []Payment{
					{Amount: 90.0, Address: "BobAccount"},    // 120 * (3/4)
					{Amount: 0.0, Address: "CharlieAccount"}, // 120 * (0/4)
					{Amount: 30.0, Address: "DavidAccount"},  // 120 * (1/4)
				},
				Output: Payment{Amount: 120.0, Address: "AliceAccount"},
			},
			expectedErr:  nil,
			expectingErr: false,
		},
		{
			name: "Error split with only one part with all zero",
			userPayment: &UserPayment{
				Name:             "ZeroPartSplit",
				Amount:           120.0,
				PrePayAddress:    "AliceAccount",
				ShouldPayAddress: []string{"BobAccount", "CharlieAccount", "DavidAccount"},
				ExtendPayMsg:     []float64{0, 0, 0}, // Total parts: 4
			},
			expectedTx: Tx{
				Name:   "ZeroPartSplit",
				Input:  []Payment{},
				Output: Payment{Amount: 120.0, Address: "AliceAccount"},
			},
			expectedErr:  fmt.Errorf("ExtendPayMsg must have a positive sum"),
			expectingErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTx, err := PartMoneySplitStrategy(tt.userPayment)

			if (err != nil) != tt.expectingErr {
				t.Errorf("PartMoneySplitStrategy() error = %v, expectingErr %v", err, tt.expectingErr)
				return
			}
			if tt.expectingErr {
				if err != nil && tt.expectedErr != nil && err.Error() != tt.expectedErr.Error() {
					t.Errorf("PartMoneySplitStrategy() error message mismatch. Got: %q, Want: %q", err.Error(), tt.expectedErr.Error())
				}
				return
			}

			if !reflect.DeepEqual(gotTx, tt.expectedTx) {
				t.Errorf("PartMoneySplitStrategy() gotTx = %v, want %v", gotTx, tt.expectedTx)
			}
		})
	}
}

func TestFixBeforeAverageMoneySplitStrategy(t *testing.T) {
	tests := []struct {
		name        string
		userPayment *UserPayment
		want        Tx
		wantErr     bool
		expectedErr string
	}{
		{
			name: "Successful split with mixed fixed and average payments",
			userPayment: &UserPayment{
				Name:             "MixedSplit",
				Amount:           100.0,
				PrePayAddress:    "Alice",
				ShouldPayAddress: []string{"Bob", "Charlie", "David"},
				ExtendPayMsg:     []float64{-20, 0, 10},
			},
			want: Tx{
				Name: "MixedSplit",
				Input: []Payment{
					{Amount: 20, Address: "Bob"},
					{Amount: 35, Address: "Charlie"},
					{Amount: 45, Address: "David"},
				},
				Output: Payment{Amount: 100.0, Address: "Alice"},
			},
			wantErr: false,
		},
		{
			name: "Successful split with all adjusted average payments",
			userPayment: &UserPayment{
				Name:             "AllAdjusted",
				Amount:           120.0,
				PrePayAddress:    "Alice",
				ShouldPayAddress: []string{"Bob", "Charlie", "David"},
				ExtendPayMsg:     []float64{10, 0, 20},
			},
			want: Tx{
				Name: "AllAdjusted",
				Input: []Payment{
					{Amount: 40, Address: "Bob"},
					{Amount: 30, Address: "Charlie"},
					{Amount: 50, Address: "David"},
				},
				Output: Payment{Amount: 120.0, Address: "Alice"},
			},
			wantErr: false,
		},
		{
			name: "Successful split with no one left to pay the average",
			userPayment: &UserPayment{
				Name:             "NoAveragePayers",
				Amount:           100.0,
				PrePayAddress:    "Alice",
				ShouldPayAddress: []string{"Bob", "Charlie"},
				ExtendPayMsg:     []float64{-50, -50},
			},
			want: Tx{
				Name: "NoAveragePayers",
				Input: []Payment{
					{Amount: 50, Address: "Bob"},
					{Amount: 50, Address: "Charlie"},
				},
				Output: Payment{Amount: 100.0, Address: "Alice"},
			},
			wantErr: false,
		},
		{
			name: "Error: lastMoney becomes negative",
			userPayment: &UserPayment{
				Name:             "NegativeLastMoney",
				Amount:           100.0,
				PrePayAddress:    "Alice",
				ShouldPayAddress: []string{"Bob", "Charlie"},
				ExtendPayMsg:     []float64{0, 110}, // 100 - 110 = -10
			},

			want:        Tx{},
			wantErr:     true,
			expectedErr: "after deducting fixed amounts, the remaining amount to be split is negative",
		},
		{
			name: "Error: ShouldPayAddress is empty",
			userPayment: &UserPayment{
				Name:             "EmptyShouldPay",
				Amount:           100.0,
				PrePayAddress:    "Alice",
				ShouldPayAddress: []string{},
				ExtendPayMsg:     []float64{},
			},
			want:        Tx{},
			wantErr:     true,
			expectedErr: "UserPayment 'EmptyShouldPay' must have at least one ShouldPayAddress for AverageSplitStrategy",
		},
		{
			name: "Error: Mismatched lengths of ExtendPayMsg and ShouldPayAddress",
			userPayment: &UserPayment{
				Name:             "MismatchedLengths",
				Amount:           100.0,
				PrePayAddress:    "Alice",
				ShouldPayAddress: []string{"Bob", "Charlie"},
				ExtendPayMsg:     []float64{-50},
			},
			want:        Tx{},
			wantErr:     true,
			expectedErr: "UserPayment 'MismatchedLengths' ExtendPayMsg must have the same length as ShouldPayAddress for AverageSplitStrategy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FixBeforeAverageMoneySplitStrategy(tt.userPayment)
			if (err != nil) != tt.wantErr {
				t.Errorf("FixBeforeAverageMoneySplitStrategy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if err.Error() != tt.expectedErr {
					t.Errorf("FixBeforeAverageMoneySplitStrategy() error message = %q, want %q", err.Error(), tt.expectedErr)
				}
				return
			}

			// Sort inputs for consistent comparison
			sort.Slice(got.Input, func(i, j int) bool {
				return got.Input[i].Address < got.Input[j].Address
			})
			sort.Slice(tt.want.Input, func(i, j int) bool {
				return tt.want.Input[i].Address < tt.want.Input[j].Address
			})

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FixBeforeAverageMoneySplitStrategy() = %v, want %v", got, tt.want)
			}
		})
	}
}
