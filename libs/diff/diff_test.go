package diff

import (
	"testing"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUIDChangelog(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	for _, test := range []struct {
		name      string
		old, next any
		kind      string
	}{
		{"same", a, a, ""},
		{"update", a, b, odiff.UPDATE},
		{"create", nil, a, odiff.CREATE},
		{"delete", a, nil, odiff.DELETE},
		{"slice create", []uuid.UUID{}, []uuid.UUID{a}, odiff.CREATE},
		{"slice delete", []uuid.UUID{a}, []uuid.UUID{}, odiff.DELETE},
		{"nil pointers", (*uuid.UUID)(nil), (*uuid.UUID)(nil), ""},
		{"pointer create", (*uuid.UUID)(nil), &a, odiff.UPDATE},
		{"pointer delete", &a, (*uuid.UUID)(nil), odiff.UPDATE},
		{"pointer update", &a, &b, odiff.UPDATE},
	} {
		t.Run(test.name, func(t *testing.T) {
			changes, err := GetCustomDiffer().Diff(test.old, test.next)
			require.NoError(t, err)
			if test.kind == "" {
				assert.Empty(t, changes)
				return
			}
			require.Len(t, changes, 1, "UUID changes must not contain byte-level diffs")
			assert.Equal(t, test.kind, changes[0].Type)
		})
	}
}

func TestUUIDSliceOrderingAndPatch(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	type value struct{ IDs []uuid.UUID }
	old, next := value{[]uuid.UUID{a, b}}, value{[]uuid.UUID{b, a}}
	differ := GetCustomDiffer()
	changes, err := differ.Diff(old, next)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	log := differ.Patch(changes, &old)
	require.False(t, log.HasErrors())
	assert.Equal(t, next, old)
}
