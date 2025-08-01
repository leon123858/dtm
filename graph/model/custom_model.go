package model

// custom define query model without some fields can let gqlgen auto generate recursive resolver
// it can improve performance and reduce unnecessary data operations
// use `make gql` to generate code

type Trip struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Record struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Amount        float64 `json:"amount"`
	Time          string  `json:"time"` // unix timestamp as string
	PrePayAddress string  `json:"prePayAddress"`
}
