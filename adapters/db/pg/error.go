package pg

import (
	"dtm/domain"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"strings"
)

func constraintError(err error) (code, name string) {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code, pe.ConstraintName
	}
	var pqe *pq.Error
	if errors.As(err, &pqe) {
		return string(pqe.Code), pqe.Constraint
	}
	return "", ""
}

func bindingError(err error) error {
	code, name := constraintError(err)
	if code == "23505" && name == "trip_bindings_address" {
		return domain.ErrConflict
	}
	if code == "23503" {
		switch name {
		case "trip_bindings_trip_id_address_id_fkey":
			return domain.ErrBadUserInput
		case "trip_bindings_user_id_fkey":
			return domain.ErrUserNotFound
		case "trip_bindings_trip_id_fkey":
			return domain.ErrTripNotFound
		}
	}
	return err
}

func addressDeleteError(err error) error {
	code, name := constraintError(err)
	if code == "23503" {
		switch name {
		case "trip_bindings_trip_id_address_id_fkey", "fk_records_trip_address", "fk_rspl_trip_address":
			return domain.ErrConflict
		}
	}
	return err
}

func subscriptionError(err error) error {
	var pqe *pq.Error
	if errors.As(err, &pqe) && pqe.Code == "23503" {
		if strings.Contains(pqe.Constraint, "user_id") {
			return domain.ErrUserNotFound
		}
		return domain.ErrTripNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23503" {
		if strings.Contains(pe.ConstraintName, "user_id") {
			return domain.ErrUserNotFound
		}
		return domain.ErrTripNotFound
	}
	return err
}
