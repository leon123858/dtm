package cmd

import (
	"dtm/domain"
	"dtm/services/tx"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var inputPath string
var outputPath string

func shareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "share",
		Short:   "accept two CSV file paths",
		Long:    `accept two CSV file paths, one for input and one for output. It will read the input CSV, validate its format, and write a sample data to the output CSV if the format is incorrect.`,
		Example: `dtm share --input input.csv --output output.csv`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputPath == "" || outputPath == "" {
				return cmd.Help()
			}

			// read the input CSV file
			inputFile, err := os.Open(inputPath)
			if err != nil {
				return err
			}
			defer func(inputFile *os.File) {
				err := inputFile.Close()
				if err != nil {
					log.Fatalf("Failed to close input file: %v", err)
				}
			}(inputFile)

			csvContent, err := csv.NewReader(inputFile).ReadAll()
			if err != nil {
				return err
			}

			payments, err := ParseCSVToUserPayments(csvContent)
			if err != nil {
				return fmt.Errorf("failed to parse CSV: %w", err)
			}
			if len(payments) == 0 {
				return fmt.Errorf("no valid user payments found in the CSV")
			}

			// create a TxPackage from the payments
			txPackage, totalRemaining, err := tx.ShareMoneyEasy(payments)
			if err != nil {
				return fmt.Errorf("failed to create TxPackage: %w", err)
			}
			if totalRemaining > 0 {
				fmt.Printf("Warning: There are remaining unspent inputs totaling %.2f\n", totalRemaining)
			}

			// write the TxPackage to the output CSV file
			outputFile, err := os.Create(outputPath)
			if err != nil {
				return err
			}
			defer func(outputFile *os.File) {
				err := outputFile.Close()
				if err != nil {
					log.Fatalf("Failed to close output file: %v", err)
				}
			}(outputFile)

			// show result in output
			_, err = outputFile.Write([]byte(txPackage.String()))
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&inputPath, "input", "i", "", "csv input file path (required)")
	err := cmd.MarkFlagRequired("input")
	if err != nil {
		log.Fatal(err)
		return nil
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "csv output file path (required)")
	err = cmd.MarkFlagRequired("output")
	if err != nil {
		log.Fatal(err)
		return nil
	}

	return cmd
}

// ParseCSVToUserPayments parses a CSV content into a slice of tx.UserPayment structs.
func ParseCSVToUserPayments(csvContent [][]string) ([]tx.UserPayment, error) {
	if len(csvContent) == 0 {
		return nil, fmt.Errorf("CSV is empty")
	}

	// skip the header row
	dataRows := csvContent[1:]

	var payments []tx.UserPayment
	addresses := make(map[string]domain.Address)
	addressForName := func(name string) domain.Address {
		if address, ok := addresses[name]; ok {
			return address
		}
		address := domain.Address{ID: uuid.New(), Name: name}
		addresses[name] = address
		return address
	}
	for i, row := range dataRows {
		if len(row) != 4 {
			return nil, fmt.Errorf("row %d: expected 4 columns, but got %d", i+2, len(row)) // +2 to account for the header row
		}

		amount, err := strconv.ParseFloat(row[1], 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: failed to convert amount '%s' to float: %w", i+2, row[1], err)
		}

		prePayName := strings.TrimSpace(row[2])
		if prePayName == "" {
			return nil, fmt.Errorf("row %d: pre-pay address is empty", i+2)
		}
		shouldPayNames := strings.Split(row[3], ",")
		shouldPayAddresses := make([]domain.Address, len(shouldPayNames))
		for j := range shouldPayNames {
			shouldPayName := strings.TrimSpace(shouldPayNames[j])
			if shouldPayName == "" {
				return nil, fmt.Errorf("row %d: should-pay address at index %d is empty", i+2, j)
			}
			shouldPayAddresses[j] = addressForName(shouldPayName)
		}

		payment := tx.UserPayment{
			Name:             row[0],
			Amount:           amount,
			PrePayAddress:    addressForName(prePayName),
			ShouldPayAddress: shouldPayAddresses,
			ExtendPayMsg:     make([]float64, len(shouldPayAddresses)), // Initialize with zero values
			PaymentType:      0,                                        // Default to AverageSplitStrategy
		}
		payments = append(payments, payment)
	}

	return payments, nil
}
