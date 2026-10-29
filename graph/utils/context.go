package utils

import (
	"context"
	"dtm/graph/model"
	"fmt"
	"github.com/99designs/gqlgen/graphql"
)

// EnclosingTrip finds the nearest trip, including its query read options.
func EnclosingTrip(ctx context.Context) (*model.Trip, error) {
	for field := graphql.GetFieldContext(ctx); field != nil; field = field.Parent {
		if trip, ok := field.Result.(*model.Trip); ok && trip != nil {
			return trip, nil
		}
	}
	return nil, fmt.Errorf("address field requires an enclosing trip")
}
