package utils

import (
	"context"
	"dtm/graph/model"
	"github.com/99designs/gqlgen/graphql"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEnclosingTrip(t *testing.T) {
	outer := &model.Trip{ID: "outer"}
	nearest := &model.Trip{ID: "nearest", HaveHistory: true}
	ctx := graphql.WithFieldContext(context.Background(), &graphql.FieldContext{Result: outer})
	ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{Result: nearest})
	ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{Result: &model.Address{}})
	got, err := EnclosingTrip(ctx)
	require.NoError(t, err)
	require.Same(t, nearest, got)
	require.True(t, got.HaveHistory)
	for _, ctx := range []context.Context{
		context.Background(),
		graphql.WithFieldContext(context.Background(), &graphql.FieldContext{Result: (*model.Trip)(nil)}),
	} {
		_, err := EnclosingTrip(ctx)
		require.Error(t, err)
	}
}
