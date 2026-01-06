package diff

import (
	"reflect"

	"github.com/google/uuid"
	odiff "github.com/r3labs/diff/v3"
)

func GetCustomDiffer() *odiff.Differ {
	ret, err := odiff.NewDiffer(odiff.CustomValueDiffers(&UUIDComparer{}))
	if err != nil {
		panic(err)
	}
	return ret
}

type UUIDComparer struct{}

var (
	uuidType = reflect.TypeOf(uuid.UUID{})
)

// Match check is field match this custom type
func (c UUIDComparer) Match(a, b reflect.Value) bool {
	aok := a.Kind() == uuidType.Kind() && a.Type() == uuidType
	bok := b.Kind() == uuidType.Kind() && b.Type() == uuidType
	return (aok && bok) || (a.Kind() == reflect.Invalid && bok) || (b.Kind() == reflect.Invalid && aok)
}

// Diff check is diff or not
func (c UUIDComparer) Diff(_ odiff.DiffType, _ odiff.DiffFunc, cl *odiff.Changelog, path []string, a reflect.Value, b reflect.Value, _ interface{}) error {
	// 取得實際數值 (處理可能為指標的情況)
	valA := reflect.Indirect(a)
	valB := reflect.Indirect(b)

	// 如果其中一個是無效值 (nil)，則視為不同
	if !valA.IsValid() || !valB.IsValid() {
		if valA.IsValid() != valB.IsValid() {
			cl.Add(odiff.UPDATE, path, a.Interface(), b.Interface())
		}
		return nil
	}

	u1 := valA.Interface().(uuid.UUID)
	u2 := valB.Interface().(uuid.UUID)

	// 核心比對邏輯
	if u1 != u2 {
		// 將其視為一個 Update 動作，而不是深層比對陣列內部的 byte
		cl.Add(odiff.UPDATE, path, u1, u2)
	}
	return nil
}

// InsertParentDiffer do something with parent，
// uuid is leaf, so do not thing
func (c UUIDComparer) InsertParentDiffer(_ func(path []string, a reflect.Value, b reflect.Value, p interface{}) error) {
	// do not thing
}
