package tx

import (
	"dtm/domain"

	"github.com/google/uuid"
)

func testAddress(name string) domain.Address {
	if name == "" {
		return domain.Address{}
	}
	return domain.Address{ID: uuid.NewSHA1(uuid.Nil, []byte(name)), Name: name}
}

func testAddresses(names ...string) []domain.Address {
	result := make([]domain.Address, len(names))
	for i, name := range names {
		result[i] = testAddress(name)
	}
	return result
}
