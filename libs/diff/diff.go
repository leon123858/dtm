package diff

import (
	"reflect"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
)

// GetCustomDiffer returns an independent differ; Differ keeps mutable state.
func GetCustomDiffer(extra ...odiff.ValueDiffer) *odiff.Differ {
	comparers := append([]odiff.ValueDiffer{&UUIDComparer{}}, extra...)
	ret, err := odiff.NewDiffer(odiff.SliceOrdering(true), odiff.CustomValueDiffers(comparers...))
	if err != nil {
		panic(err)
	}
	return ret
}

// AtomicComparer treats T as one value instead of diffing its children. This is
// useful for collections whose patch semantics are whole-field replacement.
type AtomicComparer[T any] struct{}

func (AtomicComparer[T]) Match(a, b reflect.Value) bool {
	t := reflect.TypeFor[T]()
	aok := a.IsValid() && a.Type() == t
	bok := b.IsValid() && b.Type() == t
	return (aok && bok) || (!a.IsValid() && bok) || (!b.IsValid() && aok)
}

func (AtomicComparer[T]) Diff(_ odiff.DiffType, _ odiff.DiffFunc, cl *odiff.Changelog, path []string, a, b reflect.Value, _ interface{}) error {
	switch {
	case !a.IsValid():
		cl.Add(odiff.CREATE, path, nil, b.Interface())
	case !b.IsValid():
		cl.Add(odiff.DELETE, path, a.Interface(), nil)
	case !reflect.DeepEqual(a.Interface(), b.Interface()):
		cl.Add(odiff.UPDATE, path, a.Interface(), b.Interface())
	}
	return nil
}

func (AtomicComparer[T]) InsertParentDiffer(_ func([]string, reflect.Value, reflect.Value, interface{}) error) {
}

// UUIDs are atomic values, including when a collection adds or removes one.
type UUIDComparer struct{ AtomicComparer[uuid.UUID] }
