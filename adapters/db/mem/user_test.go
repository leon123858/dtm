package mem

import (
	"testing"

	"dtm/adapters/db/testutil"
)

func TestBindingContract(t *testing.T) {
	s, a := NewInMemoryDB()
	testutil.CheckBindingContract(t, s, a)
}

func TestProfileContract(t *testing.T) {
	s, u := NewInMemoryDB()
	testutil.CheckProfileContract(t, s, u)
}
