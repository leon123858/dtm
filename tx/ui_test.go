package tx

import (
	"fmt"
	"reflect"
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
			strategy:     dummyStrategy,
			expectedTx:   Tx{},
			expectedErr:  fmt.Errorf("UserPayment 'EmptyShouldPay' must have at least one ShouldPayAddress for AverageSplitStrategy"),
			expectingErr: true,
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
