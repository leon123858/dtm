package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCSVToUserPaymentsReusesAddressIdentityByName(t *testing.T) {
	payments, err := ParseCSVToUserPayments([][]string{
		{"name", "amount", "prePayAddress", "shouldPayAddress"},
		{"Lunch", "100", "Alice", "Alice, Bob"},
		{"Taxi", "50", "Bob", "Alice"},
	})
	require.NoError(t, err)
	require.Len(t, payments, 2)
	require.Equal(t, payments[0].PrePayAddress.ID, payments[0].ShouldPayAddress[0].ID)
	require.Equal(t, payments[0].ShouldPayAddress[1].ID, payments[1].PrePayAddress.ID)
	require.Equal(t, "Alice", payments[0].PrePayAddress.Name)
}

func TestParseCSVToUserPaymentsRejectsBlankAddressNames(t *testing.T) {
	tests := []struct {
		name string
		row  []string
	}{
		{
			name: "blank pre-pay address",
			row:  []string{"Lunch", "100", "   ", "Alice"},
		},
		{
			name: "blank should-pay address",
			row:  []string{"Lunch", "100", "Alice", "Bob,   "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payments, err := ParseCSVToUserPayments([][]string{
				{"name", "amount", "prePayAddress", "shouldPayAddress"},
				tt.row,
			})
			require.Error(t, err)
			require.Nil(t, payments)
		})
	}
}
