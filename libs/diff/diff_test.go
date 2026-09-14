package diff

import (
	"testing"

	"dtm/domain"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldPayAddressDiffAndPatch(t *testing.T) {
	a := domain.RecordShare{AddressID: uuid.NewString(), ExtendMsg: 1}
	b := domain.RecordShare{AddressID: uuid.NewString(), ExtendMsg: 2}
	edited := a
	edited.ExtendMsg = 10
	for _, tc := range []struct {
		name                  string
		old, next, tail, want domain.RecordShares
		kind                  string
	}{
		{"reorder", domain.RecordShares{a, b}, domain.RecordShares{b, a}, domain.RecordShares{a, b}, domain.RecordShares{a, b}, ""},
		{"create", domain.RecordShares{b}, domain.RecordShares{b, a}, domain.RecordShares{b}, domain.RecordShares{b, a}, odiff.CREATE},
		{"update", domain.RecordShares{a, b}, domain.RecordShares{edited, b}, domain.RecordShares{b, a}, domain.RecordShares{b, edited}, odiff.UPDATE},
		{"recreate", domain.RecordShares{a, b}, domain.RecordShares{edited, b}, domain.RecordShares{b}, domain.RecordShares{b, edited}, odiff.UPDATE},
		{"recreate in empty slice", domain.RecordShares{a}, domain.RecordShares{edited}, nil, domain.RecordShares{edited}, odiff.UPDATE},
		{"delete", domain.RecordShares{a, b}, domain.RecordShares{b}, domain.RecordShares{edited, b}, domain.RecordShares{b}, odiff.DELETE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			differ := GetCustomDiffer()
			changes, err := differ.Diff(domain.RecordFields{ShouldPayAddress: tc.old}, domain.RecordFields{ShouldPayAddress: tc.next})
			require.NoError(t, err)
			if tc.kind == "" {
				require.Empty(t, changes)
			} else {
				require.Len(t, changes, 1)
				assert.Equal(t, tc.kind, changes[0].Type)
				assert.Equal(t, []string{"ShouldPayAddress", a.AddressID}, changes[0].Path)
				if tc.kind != odiff.CREATE {
					assert.Equal(t, a, changes[0].From)
				}
				if tc.kind == odiff.UPDATE {
					assert.Equal(t, edited, changes[0].To)
				}
			}
			target := domain.RecordFields{ShouldPayAddress: append(domain.RecordShares(nil), tc.tail...)}
			log := differ.Patch(changes, &target)
			require.False(t, log.HasErrors(), "%+v", log)
			assert.ElementsMatch(t, tc.want, target.ShouldPayAddress)
		})
	}
}

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
