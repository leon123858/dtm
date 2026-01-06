package mem

import (
	"context"
	"sort"
	"testing"
	"time"

	"dtm/libs/diff"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	dbt "dtm/db/db"
)

// Helper function to create a new TripInfo
func newTripInfo(name string) *dbt.TripInfo {
	return &dbt.TripInfo{
		ID:   uuid.New(),
		Name: name,
	}
}

// Helper function to create a new Record
func newRecord(name string, amount float64, prePayAddress dbt.Address, shouldPayAddresses []dbt.ExtendAddress) dbt.Record {
	return dbt.Record{
		RecordInfo: dbt.RecordInfo{
			ID:            uuid.New(),
			Name:          name,
			Amount:        amount,
			Time:          time.Now(),
			PrePayAddress: prePayAddress,
			Category:      dbt.CategoryNormal, // Default category
		},
		RecordData: dbt.RecordData{
			ShouldPayAddress: shouldPayAddresses,
		},
	}
}

func TestCreateTrip(t *testing.T) {
	db := NewInMemoryTripDBWrapper()

	t.Run("Successfully create a trip", func(t *testing.T) {
		info := newTripInfo("Trip Alpha")
		err := db.CreateTrip(info)
		assert.NoError(t, err)

		retrievedInfo, err := db.GetTripInfo(info.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrievedInfo)
		assert.Equal(t, info.ID, retrievedInfo.ID)
		assert.Equal(t, info.Name, retrievedInfo.Name)

		// Ensure TripData is initialized
		tripData, err := db.GetTripRecords(info.ID) // GetTripRecords indirectly checks TripData's records slice
		assert.NoError(t, err)
		assert.Empty(t, tripData)

		addressList, err := db.GetTripAddressList(info.ID)
		assert.NoError(t, err)
		assert.Empty(t, addressList)
	})

	t.Run("Fail to create a trip with existing ID", func(t *testing.T) {
		info := newTripInfo("Trip Beta")
		err := db.CreateTrip(info)
		assert.NoError(t, err)

		err = db.CreateTrip(info) // Try to create again with the same ID
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestCreateTripRecords(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Gamma")
	_ = db.CreateTrip(tripInfo)

	t.Run("Successfully add records to a trip", func(t *testing.T) {
		records := []dbt.Record{
			newRecord("Record 1", 100.0, "Address A", []dbt.ExtendAddress{
				{Address: "Address X", ExtendMsg: 10.0},
				{Address: "Address Y", ExtendMsg: 20.0},
			}),
			newRecord("Record 2", 50.0, "Address B", []dbt.ExtendAddress{
				{Address: "Address Z", ExtendMsg: 30.0},
			}),
		}
		err := db.CreateTripRecords(tripInfo.ID, records)
		assert.NoError(t, err)

		retrievedRecords, err := db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 2)

		// Check if record details match (compare RecordInfo only as GetTripRecords returns RecordInfo)
		assert.Contains(t, retrievedRecords, records[0].RecordInfo)
		assert.Contains(t, retrievedRecords, records[1].RecordInfo)

		assert.Equal(t, records[0].Category, dbt.CategoryNormal, "Category should be normal by default")
		assert.Equal(t, records[1].Category, dbt.CategoryNormal, "Category should be normal by default")

		// Add more records
		moreRecords := []dbt.Record{
			newRecord("Record 3", 75.0, "Address C", []dbt.ExtendAddress{
				{Address: "Address W", ExtendMsg: 15.0},
			}),
		}
		err = db.CreateTripRecords(tripInfo.ID, moreRecords)
		assert.NoError(t, err)

		retrievedRecords, err = db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 3)
		assert.Contains(t, retrievedRecords, moreRecords[0].RecordInfo)
		assert.NotEmpty(t, retrievedRecords[2].Time, "Time should be set for new records")
	})

	t.Run("Fail to add records to non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		records := []dbt.Record{newRecord("Record 4", 20.0, "Address D", nil)}
		err := db.CreateTripRecords(nonExistentID, records)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "trip with ID")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetTripInfo(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	info1 := newTripInfo("Trip Delta")
	info2 := newTripInfo("Trip Epsilon")
	_ = db.CreateTrip(info1)
	_ = db.CreateTrip(info2)

	t.Run("Successfully retrieve existing trip info", func(t *testing.T) {
		retrievedInfo, err := db.GetTripInfo(info1.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrievedInfo)
		assert.Equal(t, info1.ID, retrievedInfo.ID)
		assert.Equal(t, info1.Name, retrievedInfo.Name)
	})

	t.Run("Fail to retrieve non-existent trip info", func(t *testing.T) {
		nonExistentID := uuid.New()
		retrievedInfo, err := db.GetTripInfo(nonExistentID)
		assert.Error(t, err)
		assert.Nil(t, retrievedInfo)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetTripRecords(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Zeta")
	_ = db.CreateTrip(tripInfo)

	record1 := newRecord("Zeta Record 1", 10.0, "Addr1", []dbt.ExtendAddress{
		{Address: "Pay1", ExtendMsg: 5.0},
	})
	record2 := newRecord("Zeta Record 2", 20.0, "Addr2", []dbt.ExtendAddress{
		{Address: "Pay2", ExtendMsg: 10.0},
		{Address: "Pay3", ExtendMsg: 15.0},
	})
	_ = db.CreateTripRecords(tripInfo.ID, []dbt.Record{record1, record2})

	t.Run("Successfully retrieve trip records", func(t *testing.T) {
		retrievedRecords, err := db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 2)

		// Convert original records to RecordInfo for comparison
		expectedRecords := []dbt.RecordInfo{record1.RecordInfo, record2.RecordInfo}
		sort.Slice(retrievedRecords, func(i, j int) bool {
			return retrievedRecords[i].ID.String() < retrievedRecords[j].ID.String()
		})
		sort.Slice(expectedRecords, func(i, j int) bool {
			return expectedRecords[i].ID.String() < expectedRecords[j].ID.String()
		})
		assert.Equal(t, expectedRecords, retrievedRecords)
	})

	t.Run("Retrieve records for trip with no records", func(t *testing.T) {
		emptyTrip := newTripInfo("Empty Trip")
		_ = db.CreateTrip(emptyTrip)
		retrievedRecords, err := db.GetTripRecords(emptyTrip.ID)
		assert.NoError(t, err)
		assert.Empty(t, retrievedRecords)
	})

	t.Run("Fail to retrieve records for non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		retrievedRecords, err := db.GetTripRecords(nonExistentID)
		assert.Error(t, err)
		assert.Nil(t, retrievedRecords)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetTripAddressList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Eta")
	_ = db.CreateTrip(tripInfo)

	_ = db.TripAddressListAdd(tripInfo.ID, "Addr A")
	_ = db.TripAddressListAdd(tripInfo.ID, "Addr B")

	t.Run("Successfully retrieve trip address list", func(t *testing.T) {
		addressList, err := db.GetTripAddressList(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, addressList, 2)
		assert.Contains(t, addressList, dbt.Address("Addr A"))
		assert.Contains(t, addressList, dbt.Address("Addr B"))
	})

	t.Run("Retrieve address list for trip with no addresses", func(t *testing.T) {
		emptyTrip := newTripInfo("Empty Address Trip")
		_ = db.CreateTrip(emptyTrip)
		addressList, err := db.GetTripAddressList(emptyTrip.ID)
		assert.NoError(t, err)
		assert.Empty(t, addressList)
	})

	t.Run("Fail to retrieve address list for non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		addressList, err := db.GetTripAddressList(nonExistentID)
		assert.Error(t, err)
		assert.Nil(t, addressList)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetRecordAddressList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Theta")
	_ = db.CreateTrip(tripInfo)

	record1 := newRecord("Rec Theta 1", 10.0, "PrePay1", []dbt.ExtendAddress{
		{Address: "ShouldPay1", ExtendMsg: 5.0},
		{Address: "ShouldPay2", ExtendMsg: 10.0},
	})
	record2 := newRecord("Rec Theta 2", 20.0, "PrePay2", []dbt.ExtendAddress{
		{Address: "ShouldPay3", ExtendMsg: 15.0},
	})
	_ = db.CreateTripRecords(tripInfo.ID, []dbt.Record{record1, record2})

	t.Run("Successfully retrieve record's should pay address list", func(t *testing.T) {
		addressList, err := db.GetRecordAddressList(record1.ID)
		assert.NoError(t, err)
		assert.Len(t, addressList, 2)
		assert.Contains(t, addressList, dbt.ExtendAddress{Address: "ShouldPay1", ExtendMsg: 5.0})
		assert.Contains(t, addressList, dbt.ExtendAddress{Address: "ShouldPay2", ExtendMsg: 10.0})

		addressList, err = db.GetRecordAddressList(record2.ID)
		assert.NoError(t, err)
		assert.Len(t, addressList, 1)
		assert.Contains(t, addressList, dbt.ExtendAddress{Address: "ShouldPay3", ExtendMsg: 15.0})
	})

	t.Run("Retrieve should pay address list for record with no should pay addresses", func(t *testing.T) {
		recordEmpty := newRecord("Rec Empty", 5.0, "PrePay", nil)
		_ = db.CreateTripRecords(tripInfo.ID, []dbt.Record{recordEmpty})
		addressList, err := db.GetRecordAddressList(recordEmpty.ID)
		assert.NoError(t, err)
		assert.Empty(t, addressList)
	})

	t.Run("Fail to retrieve record's should pay address list for non-existent record", func(t *testing.T) {
		nonExistentID := uuid.New()
		addressList, err := db.GetRecordAddressList(nonExistentID)
		assert.Error(t, err)
		assert.Nil(t, addressList)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestUpdateTripInfo(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	info := newTripInfo("Original Trip Name")
	_ = db.CreateTrip(info)

	t.Run("Successfully update trip info", func(t *testing.T) {
		updatedInfo := &dbt.TripInfo{
			ID:   info.ID,
			Name: "Updated Trip Name",
		}
		err := db.UpdateTripInfo(updatedInfo)
		assert.NoError(t, err)

		retrievedInfo, err := db.GetTripInfo(info.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrievedInfo)
		assert.Equal(t, updatedInfo.Name, retrievedInfo.Name)
	})

	t.Run("Fail to update non-existent trip info", func(t *testing.T) {
		nonExistentID := uuid.New()
		updatedInfo := &dbt.TripInfo{
			ID:   nonExistentID,
			Name: "Non-existent Update",
		}
		err := db.UpdateTripInfo(updatedInfo)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found for update")
	})
}

func TestUpdateTripRecord(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Iota")
	_ = db.CreateTrip(tripInfo)

	record1 := newRecord("Rec Iota 1", 10.0, "PrePay1", []dbt.ExtendAddress{
		{Address: "PayA"},
	})
	record2 := newRecord("Rec Iota 2", 20.0, "PrePay2", []dbt.ExtendAddress{
		{Address: "PayB"},
	})
	_ = db.CreateTripRecords(tripInfo.ID, []dbt.Record{record1, record2})

	t.Run("Successfully update an existing record", func(t *testing.T) {
		updatedRecordInfo := dbt.RecordInfo{
			ID:            record1.ID,
			Name:          "Updated Rec Iota 1",
			Amount:        15.0,
			PrePayAddress: "NewPrePay1",
			Time:          time.Now(),
			Category:      dbt.CategoryFix, // Change category to Fix for this test
		}
		updatedRecord := dbt.Record{
			RecordInfo: updatedRecordInfo,
			RecordData: dbt.RecordData{
				ShouldPayAddress: []dbt.ExtendAddress{{Address: "PayU"}},
			},
		}
		cl, err := diff.GetCustomDiffer().Diff(record1, updatedRecord)
		assert.NoError(t, err)
		tripId, err := db.UpdateTripRecord(record1.ID, cl)
		assert.NoError(t, err)
		assert.Equal(t, tripInfo.ID, tripId, "Trip ID should match the original trip")

		// Retrieve records and verify
		retrievedRecords, err := db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 2)

		found := false
		for _, r := range retrievedRecords {
			if r.ID == updatedRecordInfo.ID {
				assert.Equal(t, updatedRecordInfo.Name, r.Name)
				assert.Equal(t, updatedRecordInfo.Amount, r.Amount)
				assert.Equal(t, updatedRecordInfo.PrePayAddress, r.PrePayAddress)
				assert.Equal(t, updatedRecordInfo.Category, r.Category)
				assert.LessOrEqual(t, r.Time, time.Now())
				found = true
				break
			}
		}
		assert.True(t, found, "Updated record not found in retrieved list")

		// Verify that RecordData (ShouldPayAddress)
		shouldPayList, err := db.GetRecordAddressList(record1.ID)
		assert.NoError(t, err)
		assert.Equal(t, []dbt.ExtendAddress{{Address: "PayU", ExtendMsg: 0}}, shouldPayList) // Should be updated to "PayU"
	})

	t.Run("Fail to update non-existent record", func(t *testing.T) {
		nonExistentRecordInfo := dbt.RecordInfo{
			ID:   uuid.New(),
			Name: "Non-existent Record",
		}

		cl, err := diff.GetCustomDiffer().Diff(record1, dbt.Record{
			RecordInfo: nonExistentRecordInfo,
			RecordData: dbt.RecordData{
				ShouldPayAddress: []dbt.ExtendAddress{{Address: "PayX"}},
			},
		})
		assert.NoError(t, err)
		tripId, err := db.UpdateTripRecord(nonExistentRecordInfo.ID, cl)
		assert.Error(t, err)
		assert.Equal(t, uuid.Nil, tripId, "Trip ID should be nil for non-existent record")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTripAddressListAdd(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Kappa")
	_ = db.CreateTrip(tripInfo)

	t.Run("Successfully add address to list", func(t *testing.T) {
		err := db.TripAddressListAdd(tripInfo.ID, "Address Alpha")
		assert.NoError(t, err)
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.Contains(t, list, dbt.Address("Address Alpha"))
		assert.Len(t, list, 1)

		err = db.TripAddressListAdd(tripInfo.ID, "Address Beta")
		assert.NoError(t, err)
		list, _ = db.GetTripAddressList(tripInfo.ID)
		assert.Contains(t, list, dbt.Address("Address Beta"))
		assert.Len(t, list, 2)
	})

	t.Run("Fail to add existing address", func(t *testing.T) {
		err := db.TripAddressListAdd(tripInfo.ID, "Address Alpha") // Try to add again
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.Len(t, list, 2) // Should still be 2
	})

	t.Run("Fail to add address to non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		err := db.TripAddressListAdd(nonExistentID, "Address Gamma")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTripAddressListRemove(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Lambda")
	_ = db.CreateTrip(tripInfo)
	_ = db.TripAddressListAdd(tripInfo.ID, "Address X")
	_ = db.TripAddressListAdd(tripInfo.ID, "Address Y")
	_ = db.TripAddressListAdd(tripInfo.ID, "Address Z")

	t.Run("Successfully remove address from list", func(t *testing.T) {
		err := db.TripAddressListRemove(tripInfo.ID, "Address Y")
		assert.NoError(t, err)
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.NotContains(t, list, dbt.Address("Address Y"))
		assert.Len(t, list, 2)

		err = db.TripAddressListRemove(tripInfo.ID, "Address X")
		assert.NoError(t, err)
		list, _ = db.GetTripAddressList(tripInfo.ID)
		assert.NotContains(t, list, dbt.Address("Address X"))
		assert.Len(t, list, 1)
	})

	t.Run("Fail to remove non-existent address", func(t *testing.T) {
		err := db.TripAddressListRemove(tripInfo.ID, "Address W")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.Len(t, list, 1) // Should still be 1 (Address Z)
	})

	t.Run("Fail to remove address from non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		err := db.TripAddressListRemove(nonExistentID, "Address Z")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestDeleteTrip(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	trip1 := newTripInfo("Trip Mu")
	_ = db.CreateTrip(trip1)
	record1 := newRecord("Rec Mu 1", 10.0, "P1", []dbt.ExtendAddress{{Address: "S1"}})
	_ = db.CreateTripRecords(trip1.ID, []dbt.Record{record1})
	_ = db.TripAddressListAdd(trip1.ID, "AddrM1")

	trip2 := newTripInfo("Trip Nu")
	_ = db.CreateTrip(trip2)

	t.Run("Successfully delete an existing trip", func(t *testing.T) {
		err := db.DeleteTrip(trip1.ID)
		assert.NoError(t, err)

		_, err = db.GetTripInfo(trip1.ID)
		assert.Error(t, err) // Should not find trip info
		assert.Contains(t, err.Error(), "not found")

		_, err = db.GetTripRecords(trip1.ID)
		assert.Error(t, err) // Should not find trip records
		assert.Contains(t, err.Error(), "not found")

		_, err = db.GetTripAddressList(trip1.ID)
		assert.Error(t, err) // Should not find trip address list
		assert.Contains(t, err.Error(), "not found")

		// Ensure associated record is also deleted from recordsByID map
		_, err = db.GetRecordAddressList(record1.ID) // This checks recordsByID map
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Fail to delete non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		err := db.DeleteTrip(nonExistentID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found for deletion")
	})
}

func TestDeleteTripRecord(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Xi")
	_ = db.CreateTrip(tripInfo)

	record1 := newRecord("Rec Xi 1", 10.0, "P1", []dbt.ExtendAddress{{Address: "S1"}})
	record2 := newRecord("Rec Xi 2", 20.0, "P2", []dbt.ExtendAddress{{Address: "S2"}})
	record3 := newRecord("Rec Xi 3", 30.0, "P3", []dbt.ExtendAddress{{Address: "S3"}})
	_ = db.CreateTripRecords(tripInfo.ID, []dbt.Record{record1, record2, record3})

	t.Run("Successfully delete an existing record", func(t *testing.T) {
		tripId, err := db.DeleteTripRecord(record2.ID)
		assert.NoError(t, err)
		assert.Equal(t, tripInfo.ID, tripId, "Trip ID should match the original trip")

		retrievedRecords, err := db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 2) // record2 should be gone
		assert.Contains(t, retrievedRecords, record1.RecordInfo)
		assert.Contains(t, retrievedRecords, record3.RecordInfo)
		assert.NotContains(t, retrievedRecords, record2.RecordInfo)

		// Ensure record is removed from recordsByID map
		_, err = db.GetRecordAddressList(record2.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Fail to delete non-existent record", func(t *testing.T) {
		nonExistentID := uuid.New()
		tripId, err := db.DeleteTripRecord(nonExistentID)
		assert.Error(t, err)
		assert.Empty(t, tripId)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestDataLoaderGetRecordInfoList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	ctx := context.Background()

	trip1 := newTripInfo("Trip Omicron")
	_ = db.CreateTrip(trip1)
	rec1 := newRecord("Rec Omi 1", 1.0, "P1", nil)
	rec2 := newRecord("Rec Omi 2", 2.0, "P2", nil)
	_ = db.CreateTripRecords(trip1.ID, []dbt.Record{rec1, rec2})

	trip2 := newTripInfo("Trip Pi")
	_ = db.CreateTrip(trip2)
	rec3 := newRecord("Rec Pi 1", 3.0, "P3", nil)
	_ = db.CreateTripRecords(trip2.ID, []dbt.Record{rec3})

	t.Run("Successfully load existing record infos", func(t *testing.T) {
		keys := []uuid.UUID{trip1.ID, trip2.ID}
		result, _ := db.DataLoaderGetRecordInfoList(ctx, keys)
		// assert.NoError(t, err)
		assert.Len(t, result, 2)

		assert.Contains(t, result, trip1.ID)
		assert.Len(t, result[trip1.ID], 2)
		assert.Equal(t, rec1.RecordInfo, result[trip1.ID][0])

		assert.Contains(t, result, trip2.ID)
		assert.Len(t, result[trip2.ID], 1)
		assert.Equal(t, rec3.RecordInfo, result[trip2.ID][0])
	})

	t.Run("Handle missing record infos", func(t *testing.T) {
		nonExistentID := uuid.New()
		keys := []uuid.UUID{trip1.ID, nonExistentID}
		result, err := db.DataLoaderGetRecordInfoList(ctx, keys)
		assert.Len(t, result, 2)

		assert.Contains(t, result, trip1.ID)
		assert.Len(t, result[trip1.ID], 2)
		assert.Equal(t, rec1.RecordInfo, result[trip1.ID][0])

		assert.Contains(t, result, nonExistentID)
		assert.Equal(t, result[nonExistentID], []dbt.RecordInfo{}) // Missing key should have nil value
		assert.Contains(t, err.Error(), nonExistentID.String()+" not found")
	})
}

func TestDataLoaderGetTripAddressList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	ctx := context.Background()

	trip1 := newTripInfo("Trip Rho")
	_ = db.CreateTrip(trip1)
	_ = db.TripAddressListAdd(trip1.ID, "A1")
	_ = db.TripAddressListAdd(trip1.ID, "A2")

	trip2 := newTripInfo("Trip Sigma")
	_ = db.CreateTrip(trip2)
	// No addresses for trip2

	t.Run("Successfully load existing trip address lists", func(t *testing.T) {
		keys := []uuid.UUID{trip1.ID, trip2.ID}
		result, _ := db.DataLoaderGetTripAddressList(ctx, keys)
		assert.Len(t, result, 2)

		assert.Contains(t, result, trip1.ID)
		assert.ElementsMatch(t, []dbt.Address{"A1", "A2"}, result[trip1.ID])

		assert.Contains(t, result, trip2.ID)
		assert.Empty(t, result[trip2.ID]) // Empty list for trip2
	})

	t.Run("Handle missing trip address lists", func(t *testing.T) {
		nonExistentID := uuid.New()
		keys := []uuid.UUID{trip1.ID, nonExistentID}
		result, err := db.DataLoaderGetTripAddressList(ctx, keys)
		// assert.Error(t, err)
		assert.Len(t, result, 2)

		assert.Contains(t, result, trip1.ID)
		assert.ElementsMatch(t, []dbt.Address{"A1", "A2"}, result[trip1.ID])

		assert.Contains(t, result, nonExistentID)
		assert.Equal(t, result[nonExistentID], []dbt.Address{}) // Missing key should have empty slice
		assert.Contains(t, err.Error(), nonExistentID.String()+" not found")
	})
}

func TestDataLoaderGetRecordShouldPayList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	ctx := context.Background()

	trip1 := newTripInfo("Trip Tau")
	_ = db.CreateTrip(trip1)
	rec1 := newRecord("Rec Tau 1", 100.0, "P1", []dbt.ExtendAddress{
		{Address: "SP1", ExtendMsg: 0.5},
		{Address: "SP2", ExtendMsg: 1.0},
	})
	rec2 := newRecord("Rec Tau 2", 200.0, "P2", []dbt.ExtendAddress{
		{Address: "SP3", ExtendMsg: 2.0},
	})
	rec3 := newRecord("Rec Tau 3", 300.0, "P3", nil) // No should pay addresses
	_ = db.CreateTripRecords(trip1.ID, []dbt.Record{rec1, rec2, rec3})

	t.Run("Successfully load existing record should pay lists", func(t *testing.T) {
		keys := []uuid.UUID{rec1.ID, rec2.ID, rec3.ID}
		result, _ := db.DataLoaderGetRecordShouldPayList(ctx, keys)
		// assert.NoError(t, err)
		assert.Len(t, result, 3)

		assert.Contains(t, result, rec1.ID)
		assert.ElementsMatch(t, []dbt.ExtendAddress{
			{Address: "SP1", ExtendMsg: 0.5},
			{Address: "SP2", ExtendMsg: 1.0},
		}, result[rec1.ID])
		assert.Contains(t, result, rec2.ID)
		assert.ElementsMatch(t, []dbt.ExtendAddress{
			{Address: "SP3", ExtendMsg: 2.0},
		}, result[rec2.ID])
		assert.Contains(t, result, rec3.ID)
		assert.Empty(t, result[rec3.ID]) // No should pay addresses for this record
	})

	t.Run("Handle missing record should pay lists", func(t *testing.T) {
		nonExistentID := uuid.New()
		keys := []uuid.UUID{rec1.ID, nonExistentID}
		result, err := db.DataLoaderGetRecordShouldPayList(ctx, keys)
		// assert.Error(t, err)
		assert.Len(t, result, 2)

		assert.Contains(t, result, rec1.ID)
		assert.ElementsMatch(t, []dbt.ExtendAddress{
			{Address: "SP1", ExtendMsg: 0.5},
			{Address: "SP2", ExtendMsg: 1.0},
		}, result[rec1.ID])

		assert.Contains(t, result, nonExistentID)
		assert.Equal(t, result[nonExistentID], []dbt.ExtendAddress{}) // Missing key should have empty slice
		assert.Contains(t, err.Error(), nonExistentID.String()+" not found")
	})
}

func TestDataLoaderGetTripInfoList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	ctx := context.Background()

	trip1 := newTripInfo("DataLoader Trip 1")
	trip2 := newTripInfo("DataLoader Trip 2")
	_ = db.CreateTrip(trip1)
	_ = db.CreateTrip(trip2)

	t.Run("Successfully load existing trip infos", func(t *testing.T) {
		keys := []uuid.UUID{trip1.ID, trip2.ID}
		result, _ := db.DataLoaderGetTripInfoList(ctx, keys)
		// assert.NoError(t, err)
		assert.Len(t, result, 2)

		assert.Contains(t, result, trip1.ID)
		assert.Equal(t, trip1.ID, result[trip1.ID].ID)
		assert.Equal(t, trip1.Name, result[trip1.ID].Name)

		assert.Contains(t, result, trip2.ID)
		assert.Equal(t, trip2.ID, result[trip2.ID].ID)
		assert.Equal(t, trip2.Name, result[trip2.ID].Name)
	})

	t.Run("Handle missing trip infos", func(t *testing.T) {
		nonExistentID := uuid.New()
		keys := []uuid.UUID{trip1.ID, nonExistentID}
		result, err := db.DataLoaderGetTripInfoList(ctx, keys)
		assert.Error(t, err)
		assert.Len(t, result, 2)

		assert.Contains(t, result, trip1.ID)
		assert.Equal(t, trip1.ID, result[trip1.ID].ID)
		assert.Equal(t, trip1.Name, result[trip1.ID].Name)

		assert.Contains(t, result, nonExistentID)
		assert.Nil(t, result[nonExistentID])
		assert.Contains(t, err.Error(), nonExistentID.String())
	})
}
