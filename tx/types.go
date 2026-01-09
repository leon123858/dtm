package tx

// Threshold for float comparisons
const epsilon = 1e-9

// UserPayment represents a user's intention to pay, with a single source and multiple potential destinations.
type UserPayment struct {
	Name             string    // A descriptive name for this user payment
	Amount           float64   // The total amount the user is paying
	PrePayAddress    string    // The address from which the payment originates (pre-payment)
	ShouldPayAddress []string  // A list of addresses that should receive a share of the payment
	ExtendPayMsg     []float64 // Additional messages or metadata associated with each should-pay address
	PaymentType      int       // let inner module choose strategy to calculate result
}

// Payment represents a single payment with an amount and an address.
type Payment struct {
	Amount  float64
	Address string
}

// Tx represents a transaction.
type Tx struct {
	Input  []Payment // input is a slice of Payment structs
	Output Payment   // output is a single Payment struct
	Name   string    // name is a string
}

// Package represents a package containing multiple transactions.
type Package struct {
	Name   string // Name of the transaction package
	TxList []Tx   // A slice of Tx (transaction) structs
}

// Cash represents the net financial movement for a specific address.
type Cash struct {
	Address      string  // The address identifier
	InputAmount  float64 // Total amount received by this address (as an output in other transactions)
	OutputAmount float64 // Total amount sent from this address (as an input in other transactions)
}

// UserPaymentToTxStrategy defines the interface for converting a UserPayment into a Tx.
// It takes the UserPayment and returns a Tx struct, or an error if conversion fails.
type UserPaymentToTxStrategy func(up *UserPayment) (Tx, error)

// ListGenerateStrategy is a strategy for converting UserPayment to Tx by averaging the payment among recipients.
type ListGenerateStrategy func(txList *[]Tx, cashList *[]Cash) (float64, error)
