package trip

import (
	"errors"
	"fmt"

	"dtm/adapters/db/db"
)

// Translate persistence classifications at the object boundary while retaining
// the original error for diagnostics and errors.Is checks.
func fromStoreError(err error) error {
	if err == nil {
		return nil
	}
	for _, pair := range [][2]error{
		{db.ErrRecordNotFound, ErrRecordNotFound},
		{db.ErrTripNotFound, ErrTripNotFound},
		{db.ErrInvalidChain, ErrInvalidChain},
	} {
		if errors.Is(err, pair[0]) {
			if errors.Is(err, pair[1]) {
				return err
			}
			return fmt.Errorf("%w: %w", pair[1], err)
		}
	}
	return err
}
