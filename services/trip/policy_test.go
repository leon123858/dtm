package trip

import (
	"errors"
	"math"
	"testing"

	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordPolicyPrepareNewCanonicalizesAndValidates(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	value := testPayment(uuid.New(), domain.Address{ID: payer.ID, Name: "stale"}, domain.Address{ID: member.ID, Name: "stale"})

	got, err := (recordPolicy{}).PrepareNew(value, []domain.Address{payer, member})
	require.NoError(t, err)
	assert.Equal(t, payer, got.PrePayAddress)
	assert.Equal(t, member, got.ShouldPayAddress[0].Address)
	assert.Equal(t, "stale", value.PrePayAddress.Name, "policy must not mutate caller-owned data")
}

func TestRecordPolicyReturnsStableValidationErrors(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	valid := testPayment(uuid.New(), payer, member)

	tests := []struct {
		name   string
		change func(*domain.Record)
		want   error
	}{
		{name: "missing ID", change: func(v *domain.Record) { v.ID = uuid.Nil }, want: ErrInvalidRecordSnapshot},
		{name: "unsafe name", change: func(v *domain.Record) { v.Name = "<meal>" }, want: ErrInvalidRecordSnapshot},
		{name: "non-positive amount", change: func(v *domain.Record) { v.Amount = 0 }, want: ErrInvalidRecordSnapshot},
		{name: "unknown category", change: func(v *domain.Record) { v.Category = domain.RecordCategory(99) }, want: ErrInvalidRecordSnapshot},
		{name: "no recipients", change: func(v *domain.Record) { v.ShouldPayAddress = nil }, want: ErrInvalidRecordSnapshot},
		{name: "duplicate recipients", change: func(v *domain.Record) { v.ShouldPayAddress = append(v.ShouldPayAddress, v.ShouldPayAddress[0]) }, want: ErrInvalidRecordAddresses},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := cloneDomainRecord(valid)
			tt.change(&value)
			err := (recordPolicy{}).Validate(value)
			require.ErrorIs(t, err, tt.want)
			assert.True(t, errors.Is(err, ErrInvalidRecordSnapshot))
		})
	}
}

func TestRecordPolicyRejectsDeletedCreateAndForeignAddress(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	value := testPayment(uuid.New(), payer, member)
	value.IsDeleted = true

	_, err := (recordPolicy{}).PrepareNew(value, []domain.Address{payer, member})
	require.ErrorIs(t, err, ErrInvalidRecordSnapshot)

	value.IsDeleted = false
	_, err = (recordPolicy{}).PrepareNew(value, []domain.Address{payer})
	require.ErrorIs(t, err, ErrInvalidRecordAddresses)
	require.ErrorIs(t, err, ErrInvalidRecordSnapshot)
}

func TestRecordPolicyAllowsInvalidMergedSharesForLaterRepair(t *testing.T) {
	for _, scenario := range []string{"no recipients", "unbalanced fixed amounts"} {
		t.Run(scenario, func(t *testing.T) {
			a, b := domain.Address{ID: uuid.New(), Name: "A"}, domain.Address{ID: uuid.New(), Name: "B"}
			base := testPayment(uuid.New(), a, b)
			base.ShouldPayAddress = []domain.ExtendAddress{{Address: a, ExtendMsg: 10}, {Address: b, ExtendMsg: 10}}
			if scenario == "unbalanced fixed amounts" {
				base.Category = domain.CategoryFix
			}
			left, right := cloneDomainRecord(base), cloneDomainRecord(base)
			if scenario == "no recipients" {
				left.ShouldPayAddress = left.ShouldPayAddress[:1]
				right.ShouldPayAddress = right.ShouldPayAddress[1:]
			} else {
				left.Amount = 30
				left.ShouldPayAddress[0].ExtendMsg = 20
				right.Amount = 40
				right.ShouldPayAddress[1].ExtendMsg = 30
			}
			policy := recordPolicy{}
			require.NoError(t, policy.Validate(left))
			require.NoError(t, policy.Validate(right))
			first := policyTestPatch(t, base, func(v *domain.Record) { *v = left })
			second := policyTestPatch(t, base, func(v *domain.Record) { *v = right })
			tail, _, err := policy.ApplyPatch(base, first, []domain.Address{a, b})
			require.NoError(t, err)
			merged, changed, err := policy.ApplyPatch(tail, second, []domain.Address{a, b})
			require.NoError(t, err)
			assert.True(t, changed)
			want := cloneDomainRecord(right)
			if scenario == "no recipients" {
				want.ShouldPayAddress = nil
			} else {
				want.ShouldPayAddress[0].ExtendMsg = 20
			}
			assert.ElementsMatch(t, want.ShouldPayAddress, merged.ShouldPayAddress)
			valid, err := NewRecordFactory(nil).FromRecord(merged).Validate()
			require.NoError(t, err)
			assert.False(t, valid)
			// Also exercise post-merge validation independently of the atomic-diff bug.
			invalidPatch := policyTestPatch(t, tail, func(v *domain.Record) { *v = want })
			t.Run("materialize invalid result", func(t *testing.T) {
				invalid, _, err := policy.ApplyPatch(tail, invalidPatch, []domain.Address{a, b})
				require.NoError(t, err)
				valid, err := NewRecordFactory(nil).FromRecord(invalid).Validate()
				require.NoError(t, err)
				assert.False(t, valid)
			})
		})
	}
}

func TestRecordPolicyPatchKeepsFieldValidation(t *testing.T) {
	a := domain.Address{ID: uuid.New(), Name: "A"}
	base := testPayment(uuid.New(), a, a)
	for _, tc := range []struct {
		name string
		edit func(*domain.Record)
	}{
		{"name", func(r *domain.Record) { r.Name = "<bad>" }},
		{"amount", func(r *domain.Record) { r.Amount = 0 }},
		{"nonfinite amount", func(r *domain.Record) { r.Amount = math.NaN() }},
		{"category", func(r *domain.Record) { r.Category = domain.RecordCategory(99) }},
		{"foreign payer", func(r *domain.Record) { r.PrePayAddress.ID = uuid.New() }},
		{"foreign share", func(r *domain.Record) { r.ShouldPayAddress[0].Address.ID = uuid.New() }},
		{"member limit", func(r *domain.Record) {
			for i := 0; i < 101; i++ {
				r.ShouldPayAddress = append(r.ShouldPayAddress, domain.ExtendAddress{Address: domain.Address{ID: uuid.New()}})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := cloneDomainRecord(base)
			tc.edit(&next)
			addresses := []domain.Address{a}
			if tc.name == "member limit" {
				for _, s := range next.ShouldPayAddress {
					addresses = append(addresses, s.Address)
				}
			}
			patch := policyTestPatch(t, base, func(r *domain.Record) { *r = next })
			_, _, err := (recordPolicy{}).ApplyPatch(base, patch, addresses)
			require.ErrorIs(t, err, ErrInvalidRecordSnapshot)
		})
	}
	for _, scenario := range []string{"empty", "unbalanced"} {
		t.Run("create/"+scenario, func(t *testing.T) {
			next := cloneDomainRecord(base)
			if scenario == "empty" {
				next.ShouldPayAddress = nil
			} else {
				next.Category = domain.CategoryFix
				next.ShouldPayAddress[0].ExtendMsg = next.Amount + 1
			}
			_, err := (recordPolicy{}).PrepareNew(next, []domain.Address{a})
			require.ErrorIs(t, err, ErrInvalidRecordSnapshot)
		})
	}
}
