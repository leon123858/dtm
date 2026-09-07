package mem

import (
	"context"
	"dtm/adapters/db/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDeleteAddressProtectsPayerAndRecipient(t *testing.T) {
	database, tripID, payer, member := setupTrip(t)
	_, err := database.AppendNew(context.Background(), tripID, paymentRecord(uuid.New(), payer, member), testutil.Materializer{})
	require.NoError(t, err)
	for _, id := range []uuid.UUID{payer.ID, member.ID} {
		result, err := database.DeleteAddress(tripID, id)
		require.ErrorContains(t, err, "referenced")
		require.Nil(t, result)
		require.Len(t, database.tripsData[tripID].AddressList, 2)
		require.Len(t, database.tripsData[tripID].Records, 1)
	}
	spare, err := database.CreateAddress(tripID, "unused")
	require.NoError(t, err)
	removed, err := database.DeleteAddress(tripID, spare.ID)
	require.NoError(t, err)
	require.Equal(t, spare, removed)
	require.Len(t, database.tripsData[tripID].AddressList, 2)
	_, err = database.DeleteAddress(tripID, spare.ID)
	require.ErrorContains(t, err, "not found")
	_, err = database.DeleteAddress(uuid.New(), payer.ID)
	require.ErrorContains(t, err, "not found")
}
