package model

type Address struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Trip retains read options for its field resolvers.

type Trip struct {
	HaveHistory bool   `json:"-"`
	ID          string `json:"id"`
	Name        string `json:"name"`
}

type Record struct {
	ShouldPayAddress []*Address     `json:"shouldPayAddress"`
	ExtendPayMsg     []float64      `json:"extendPayMsg"`
	IsValid          bool           `json:"isValid"`
	ID               string         `json:"id"`
	Name             *string        `json:"name"`
	Amount           *float64       `json:"amount"`
	Time             string         `json:"time"` // unix timestamp as string
	PrePayAddress    *Address       `json:"prePayAddress"`
	Category         RecordCategory `json:"category"`
	ParentRecordID   *string        `json:"parentRecordId"`
	IsDeleted        bool           `json:"isDeleted"`
	IsActive         bool           `json:"isActive"`
}
