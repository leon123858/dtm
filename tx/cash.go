package tx

import (
	"container/list"
	"fmt"
	"math"
	"sort"
)

// NormalizeCash aggregates the cash movements for each address.
// It combines multiple entries for the same address into a single entry,
// let cash will only have input or output amounts, not both.
func NormalizeCash(cashList []Cash) []Cash {
	// Create a map to aggregate amounts by address
	addressMap := make(map[string]*Cash)

	for _, cash := range cashList {
		if entry, exists := addressMap[cash.Address]; exists {
			entry.InputAmount += cash.InputAmount
			entry.OutputAmount += cash.OutputAmount
		} else {
			addressMap[cash.Address] = &Cash{
				Address:      cash.Address,
				InputAmount:  cash.InputAmount,
				OutputAmount: cash.OutputAmount,
			}
		}
	}

	// merge input and output amounts
	result := make([]Cash, 0, len(addressMap))
	for _, entry := range addressMap {
		if entry.OutputAmount > entry.InputAmount {
			entry.OutputAmount -= entry.InputAmount
			entry.InputAmount = 0.0
		} else {
			entry.InputAmount -= entry.OutputAmount
			entry.OutputAmount = 0.0
		}
		if entry.InputAmount < epsilon {
			entry.InputAmount = 0
		}
		if entry.OutputAmount < epsilon {
			entry.OutputAmount = 0
		}

		result = append(result, *entry)
	}

	return result
}

// generateQueues put cash into 2 sorted queues, split by input and output
func generateQueues(cashList []Cash) (*list.List, *list.List) {
	// Use Go's `container/list` as a double-ended queue (deque)
	// We'll populate temporary slices first, then sort, then push to queues.
	var tempInputSlice []Cash
	var tempOutputSlice []Cash

	// Pre-process cashList to populate temporary slices
	for _, cash := range cashList {
		if cash.InputAmount > epsilon && cash.InputAmount > cash.OutputAmount { // Only push if there's actual input
			tempInputSlice = append(tempInputSlice, cash)
		} else if cash.OutputAmount > epsilon && cash.OutputAmount > cash.InputAmount { // Only push if there's actual output
			tempOutputSlice = append(tempOutputSlice, cash)
		}
		// If both are zero or negative, or one is positive and other negative, it's ignored for this process
	}

	// sort the input slice by InputAmount, descending, and by address for stable sorting
	sort.SliceStable(tempInputSlice, func(i, j int) bool {
		// Sort by address to ensure stable sorting for same InputAmount
		if tempInputSlice[i].InputAmount == tempInputSlice[j].InputAmount {
			return tempInputSlice[i].Address < tempInputSlice[j].Address // Ascending order by address
		}
		return tempInputSlice[i].InputAmount > tempInputSlice[j].InputAmount // Descending order by InputAmount
	})
	// Sort the output slice by OutputAmount, descending, and by address for stable sorting
	sort.SliceStable(tempOutputSlice, func(i, j int) bool {
		// Sort by address to ensure stable sorting for same OutputAmount
		if tempOutputSlice[i].OutputAmount == tempOutputSlice[j].OutputAmount {
			return tempOutputSlice[i].Address < tempOutputSlice[j].Address // Ascending order by address
		}
		return tempOutputSlice[i].OutputAmount > tempOutputSlice[j].OutputAmount // Descending order by OutputAmount
	})

	// Repopulate actual queues from sorted slices
	inputQueue := list.New()
	for _, cash := range tempInputSlice {
		inputQueue.PushBack(cash)
	}

	outputQueue := list.New()
	for _, cash := range tempOutputSlice {
		outputQueue.PushBack(cash)
	}

	return inputQueue, outputQueue
}

// PrintCash prints the cash movements for each address in a human-readable format.
// It checks if both input and output amounts are present, and prints accordingly.
func PrintCash(cashList []Cash) {
	for _, cash := range cashList {
		if cash.InputAmount > 0 && cash.OutputAmount > 0 {
			// If both input and output amounts are present, print both
			fmt.Printf("Address: %s, Input: %.0f, Output: %.0f\n", cash.Address, cash.InputAmount, cash.OutputAmount)
		} else if cash.InputAmount > 0 {
			// If only input amount is present, print input
			fmt.Printf("Address: %s, Input: %.0f\n", cash.Address, cash.InputAmount)
		} else if cash.OutputAmount > 0 {
			// If only output amount is present, print output
			fmt.Printf("Address: %s, Output: %.0f\n", cash.Address, cash.OutputAmount)
		}
	}
}

func ListTxGenerateWithMixMap(txList *[]Tx, cashList *[]Cash) (float64, error) {
	var totalRemainingInputAmount float64 = 0.0
	var inputQueue, outputQueue *list.List = generateQueues(*cashList)

	// Process transactions until all outputs are covered or inputs are exhausted

	for outputQueue.Len() > 0 {
		currentOutputElem := outputQueue.Front()
		if currentOutputElem == nil {
			break
		}
		outputQueue.Remove(currentOutputElem)
		currentOutputCash := currentOutputElem.Value.(Cash) // Type assertion

		// If for some reason output becomes zero or less (shouldn't happen with pre-processing), skip
		if currentOutputCash.OutputAmount <= epsilon {
			fmt.Printf("Warning: Output for %s is zero or negative, skipping.\n", currentOutputCash.Address)
			continue // Skip this output
		}

		// Collect inputs to cover the current output
		var collectedInputs []Payment
		var currentInputSum float64 = 0.0

		for inputQueue.Len() > 0 && currentInputSum < currentOutputCash.OutputAmount {
			currentInputElem := inputQueue.Front()
			inputQueue.Remove(currentInputElem)
			if currentInputElem == nil {
				break
			}
			currentInputCash := currentInputElem.Value.(Cash) // Type assertion

			// This is an 'input' for the transaction, so it's an 'output' from the address's perspective
			collectedInputs = append(collectedInputs, Payment{
				Amount:  currentInputCash.InputAmount,
				Address: currentInputCash.Address,
			})
			currentInputSum += currentInputCash.InputAmount
		}

		// We have enough or more inputs to cover currentOutputCash.OutputAmount
		txOutputPayment := Payment{
			Amount:  currentOutputCash.OutputAmount,
			Address: currentOutputCash.Address,
		}

		// Handle the case where collected inputs are exactly equal to output or greater
		if math.Abs(currentInputSum-currentOutputCash.OutputAmount) < epsilon {
			// Inputs sum equals output. Use all collected inputs.
			*txList = append(*txList, Tx{
				Name:   fmt.Sprintf("Tx_M_to_%s", currentOutputCash.Address), // Simple naming
				Input:  collectedInputs,
				Output: txOutputPayment,
			})
		} else if currentInputSum < currentOutputCash.OutputAmount {
			// when input can not cover output
			// This condition should not happen due to pre-processing, but let's handle it gracefully
			return totalRemainingInputAmount, fmt.Errorf("unexpected condition: collected inputs sum %.2f is less than output %.2f for %s",
				currentInputSum, currentOutputCash.OutputAmount, currentOutputCash.Address)
		} else { // currentInputSum > currentOutputCash.OutputAmount
			// Inputs sum is greater than output. We need to split the last input.
			lastInputPayment := collectedInputs[len(collectedInputs)-1]
			collectedInputs = collectedInputs[:len(collectedInputs)-1] // Remove the last one

			// Amount needed from the last input to exactly cover the output
			amountNeededFromLastInput := currentOutputCash.OutputAmount - (currentInputSum - lastInputPayment.Amount)

			// The part of the last input that goes to the output
			inputPartForTx := Payment{
				Amount:  amountNeededFromLastInput,
				Address: lastInputPayment.Address,
			}
			collectedInputs = append(collectedInputs, inputPartForTx)

			// The remaining part of the last input goes back to the input queue
			remainingAmount := lastInputPayment.Amount - amountNeededFromLastInput
			if remainingAmount > epsilon { // Only push back if there's a significant remainder
				inputQueue.PushBack(Cash{
					Address:      lastInputPayment.Address,
					InputAmount:  remainingAmount, // This cash represents an available input
					OutputAmount: 0.0,
				})
			}

			// Create the transaction
			*txList = append(*txList, Tx{
				Name:   fmt.Sprintf("Tx_M_to_%s", currentOutputCash.Address), // Simple naming
				Input:  collectedInputs,
				Output: txOutputPayment,
			})
		}
	}

	// Any remaining inputs in the input queue are considered "unspent" or "leftover"

	for inputQueue.Len() > 0 {
		inputElem := inputQueue.Front()
		if inputElem == nil {
			break
		}
		inputQueue.Remove(inputElem)
		inputCash := inputElem.Value.(Cash)
		totalRemainingInputAmount += inputCash.InputAmount
	}

	return totalRemainingInputAmount, nil
}

// CashListToTxPackage converts a slice of Cash objects into a TxPackage,
// forming transactions based on the specified queue algorithm.
// It returns the generated TxPackage and the total remaining input amount.
func CashListToTxPackage(cashList []Cash, packageName string, strategy ListGenerateStrategy) (Package, float64, error) {
	var generatedTxList []Tx
	totalRemainingInputAmount, err := strategy(&generatedTxList, &cashList)
	if err != nil {
		return Package{}, 0, err
	}
	if totalRemainingInputAmount > epsilon {
		fmt.Printf("Warning: There are remaining unspent inputs totaling %.2f\n", totalRemainingInputAmount)
		return Package{}, totalRemainingInputAmount, fmt.Errorf("there are remaining unspent inputs totaling %.2f", totalRemainingInputAmount)
	}

	return Package{
		Name:   packageName,
		TxList: generatedTxList,
	}, totalRemainingInputAmount, nil
}
