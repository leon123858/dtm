// Package testutil provides deterministic append callbacks for adapter tests.
package testutil

import (
	"reflect"

	"dtm/domain"
)

// Materializer exercises storage mechanics independently of business policy.
type Materializer struct{ Err error }

func (m Materializer) PrepareNew(value domain.Record, _ []domain.Address) (domain.Record, error) {
	return value, m.Err
}

func (m Materializer) ApplyPatch(value domain.Record, patch domain.RecordPatch, _ []domain.Address) (domain.Record, bool, error) {
	result := value
	if patch.Name != nil {
		result.Name = *patch.Name
	}
	if patch.Amount != nil {
		result.Amount = *patch.Amount
	}
	if patch.IsDeleted != nil {
		result.IsDeleted = *patch.IsDeleted
	}
	return result, !reflect.DeepEqual(value, result), m.Err
}
