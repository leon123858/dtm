// Package testutil provides deterministic append callbacks for adapter tests.
package testutil

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"dtm/domain"
	"dtm/libs/recordpatch"
)

// Materializer exercises storage mechanics independently of business policy.
type Materializer struct{ Err error }

func (m Materializer) PrepareNew(value domain.Record, _ []domain.Address) (domain.Record, error) {
	return value, m.Err
}

func (m Materializer) ApplyPatch(value domain.Record, patch domain.RecordPatch, _ []domain.Address) (domain.Record, bool, error) {
	if m.Err != nil {
		return domain.Record{}, false, m.Err
	}
	result, err := recordpatch.Apply(value, patch)
	return result, !reflect.DeepEqual(value, result), err
}

func Patch(t *testing.T, old, next domain.RecordFields) domain.RecordPatch {
	t.Helper()
	patch, err := recordpatch.Diff(old, next)
	require.NoError(t, err)
	return patch
}
