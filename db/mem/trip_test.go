package mem

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"dtm/db/db"
	"dtm/domain"
	"dtm/libs/diff"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Helper function to create a new TripInfo
func newTripInfo(name string) *domain.TripInfo {
	return &domain.TripInfo{
		ID:   uuid.New(),
		Name: name,
	}
}

func addr(name string) domain.Address {
	return domain.Address{ID: uuid.NewSHA1(uuid.Nil, []byte(name)), Name: name}
}

func createRecords(t *testing.T, wrapper db.TripDBWrapper, tripID uuid.UUID, records []domain.Record) error {
	t.Helper()
	memory := wrapper.(*inMemoryTripDBWrapper)
	memory.mu.Lock()
	tripData, exists := memory.tripsData[tripID]
	if !exists {
		memory.mu.Unlock()
		return fmt.Errorf("trip with ID %s not found", tripID)
	}
	known := make(map[uuid.UUID]struct{}, len(tripData.AddressList))
	for _, address := range tripData.AddressList {
		known[address.ID] = struct{}{}
	}
	add := func(address domain.Address) {
		if _, ok := known[address.ID]; !ok {
			tripData.AddressList = append(tripData.AddressList, address)
			known[address.ID] = struct{}{}
		}
	}
	for _, record := range records {
		add(record.PrePayAddress)
		for _, address := range record.ShouldPayAddress {
			add(address.Address)
		}
	}
	memory.mu.Unlock()
	return wrapper.CreateTripRecords(tripID, records)
}

// Helper function to create a new Record
func newRecord(name string, amount float64, prePayAddress domain.Address, shouldPayAddresses []domain.ExtendAddress) domain.Record {
	return domain.Record{
		RecordInfo: domain.RecordInfo{
			ID:            uuid.New(),
			Name:          name,
			Amount:        amount,
			Time:          time.Now(),
			PrePayAddress: prePayAddress,
			Category:      domain.CategoryNormal, // Default category
		},
		RecordData: domain.RecordData{
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
		records := []domain.Record{
			newRecord("Record 1", 100.0, addr("Address A"), []domain.ExtendAddress{
				{Address: addr("Address X"), ExtendMsg: 10.0},
				{Address: addr("Address Y"), ExtendMsg: 20.0},
			}),
			newRecord("Record 2", 50.0, addr("Address B"), []domain.ExtendAddress{
				{Address: addr("Address Z"), ExtendMsg: 30.0},
			}),
		}
		err := createRecords(t, db, tripInfo.ID, records)
		assert.NoError(t, err)

		retrievedRecords, err := db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 2)

		// Check if record details match (compare RecordInfo only as GetTripRecords returns RecordInfo)
		assert.Contains(t, retrievedRecords, records[0].RecordInfo)
		assert.Contains(t, retrievedRecords, records[1].RecordInfo)

		assert.Equal(t, records[0].Category, domain.CategoryNormal, "Category should be normal by default")
		assert.Equal(t, records[1].Category, domain.CategoryNormal, "Category should be normal by default")

		// Add more records
		moreRecords := []domain.Record{
			newRecord("Record 3", 75.0, addr("Address C"), []domain.ExtendAddress{
				{Address: addr("Address W"), ExtendMsg: 15.0},
			}),
		}
		err = createRecords(t, db, tripInfo.ID, moreRecords)
		assert.NoError(t, err)

		retrievedRecords, err = db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 3)
		assert.Contains(t, retrievedRecords, moreRecords[0].RecordInfo)
		assert.NotEmpty(t, retrievedRecords[2].Time, "Time should be set for new records")
	})

	t.Run("Fail to add records to non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		records := []domain.Record{newRecord("Record 4", 20.0, addr("Address D"), nil)}
		err := createRecords(t, db, nonExistentID, records)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "trip with ID")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestCreateTripRecordsRejectsBatchAtomically(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	trip := newTripInfo("Atomic batch")
	otherTrip := newTripInfo("Other atomic batch")
	assert.NoError(t, db.CreateTrip(trip))
	assert.NoError(t, db.CreateTrip(otherTrip))

	prePay, err := db.CreateAddress(trip.ID, "Atomic pre-pay")
	assert.NoError(t, err)
	shouldPay, err := db.CreateAddress(trip.ID, "Atomic should-pay")
	assert.NoError(t, err)
	foreign, err := db.CreateAddress(otherTrip.ID, "Foreign atomic address")
	assert.NoError(t, err)

	valid := newRecord(
		"Valid record",
		10,
		*prePay,
		[]domain.ExtendAddress{{Address: *shouldPay}},
	)
	invalid := newRecord(
		"Invalid record",
		20,
		*prePay,
		[]domain.ExtendAddress{{Address: *foreign}},
	)

	err = db.CreateTripRecords(trip.ID, []domain.Record{valid, invalid})
	assert.Error(t, err)
	records, getErr := db.GetTripRecords(trip.ID)
	assert.NoError(t, getErr)
	assert.Empty(t, records, "failed batch must not persist earlier valid records")
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

	record1 := newRecord("Zeta Record 1", 10.0, addr("Addr1"), []domain.ExtendAddress{
		{Address: addr("Pay1"), ExtendMsg: 5.0},
	})
	record2 := newRecord("Zeta Record 2", 20.0, addr("Addr2"), []domain.ExtendAddress{
		{Address: addr("Pay2"), ExtendMsg: 10.0},
		{Address: addr("Pay3"), ExtendMsg: 15.0},
	})
	_ = createRecords(t, db, tripInfo.ID, []domain.Record{record1, record2})

	t.Run("Successfully retrieve trip records", func(t *testing.T) {
		retrievedRecords, err := db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, retrievedRecords, 2)

		// Convert original records to RecordInfo for comparison
		expectedRecords := []domain.RecordInfo{record1.RecordInfo, record2.RecordInfo}
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

	addrA, _ := db.CreateAddress(tripInfo.ID, "Addr A")
	addrB, _ := db.CreateAddress(tripInfo.ID, "Addr B")

	t.Run("Successfully retrieve trip address list", func(t *testing.T) {
		addressList, err := db.GetTripAddressList(tripInfo.ID)
		assert.NoError(t, err)
		assert.Len(t, addressList, 2)
		assert.Contains(t, addressList, *addrA)
		assert.Contains(t, addressList, *addrB)
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

	record1 := newRecord("Rec Theta 1", 10.0, addr("PrePay1"), []domain.ExtendAddress{
		{Address: addr("ShouldPay1"), ExtendMsg: 5.0},
		{Address: addr("ShouldPay2"), ExtendMsg: 10.0},
	})
	record2 := newRecord("Rec Theta 2", 20.0, addr("PrePay2"), []domain.ExtendAddress{
		{Address: addr("ShouldPay3"), ExtendMsg: 15.0},
	})
	_ = createRecords(t, db, tripInfo.ID, []domain.Record{record1, record2})

	t.Run("Successfully retrieve record's should pay address list", func(t *testing.T) {
		recordTripID, err := db.GetRecordTripID(record1.ID)
		assert.NoError(t, err)
		assert.Equal(t, tripInfo.ID, recordTripID)

		addressList, err := db.GetRecordAddressList(record1.ID)
		assert.NoError(t, err)
		assert.Len(t, addressList, 2)
		assert.Contains(t, addressList, domain.ExtendAddress{Address: addr("ShouldPay1"), ExtendMsg: 5.0})
		assert.Contains(t, addressList, domain.ExtendAddress{Address: addr("ShouldPay2"), ExtendMsg: 10.0})

		addressList, err = db.GetRecordAddressList(record2.ID)
		assert.NoError(t, err)
		assert.Len(t, addressList, 1)
		assert.Contains(t, addressList, domain.ExtendAddress{Address: addr("ShouldPay3"), ExtendMsg: 15.0})
	})

	t.Run("Retrieve should pay address list for record with no should pay addresses", func(t *testing.T) {
		recordEmpty := newRecord("Rec Empty", 5.0, addr("PrePay"), nil)
		_ = createRecords(t, db, tripInfo.ID, []domain.Record{recordEmpty})
		addressList, err := db.GetRecordAddressList(recordEmpty.ID)
		assert.NoError(t, err)
		assert.Empty(t, addressList)
	})

	t.Run("Fail to retrieve record's should pay address list for non-existent record", func(t *testing.T) {
		nonExistentID := uuid.New()
		recordTripID, tripErr := db.GetRecordTripID(nonExistentID)
		assert.Error(t, tripErr)
		assert.Equal(t, uuid.Nil, recordTripID)

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
		updatedInfo := &domain.TripInfo{
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
		updatedInfo := &domain.TripInfo{
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

	record1 := newRecord("Rec Iota 1", 10.0, addr("PrePay1"), []domain.ExtendAddress{
		{Address: addr("PayA")},
	})
	record2 := newRecord("Rec Iota 2", 20.0, addr("PrePay2"), []domain.ExtendAddress{
		{Address: addr("PayB")},
	})
	_ = createRecords(t, db, tripInfo.ID, []domain.Record{record1, record2})

	t.Run("Successfully update an existing record", func(t *testing.T) {
		newPrePay, err := db.CreateAddress(tripInfo.ID, "NewPrePay1")
		assert.NoError(t, err)
		payU, err := db.CreateAddress(tripInfo.ID, "PayU")
		assert.NoError(t, err)
		updatedRecordInfo := domain.RecordInfo{
			ID:            record1.ID,
			Name:          "Updated Rec Iota 1",
			Amount:        15.0,
			PrePayAddress: *newPrePay,
			Time:          time.Now(),
			Category:      domain.CategoryFix, // Change category to Fix for this test
		}
		updatedRecord := domain.Record{
			RecordInfo: updatedRecordInfo,
			RecordData: domain.RecordData{
				ShouldPayAddress: []domain.ExtendAddress{{Address: *payU}},
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
		assert.Equal(t, []domain.ExtendAddress{{Address: *payU, ExtendMsg: 0}}, shouldPayList)
	})

	t.Run("Fail to update non-existent record", func(t *testing.T) {
		nonExistentRecordInfo := domain.RecordInfo{
			ID:   uuid.New(),
			Name: "Non-existent Record",
		}

		cl, err := diff.GetCustomDiffer().Diff(record1, domain.Record{
			RecordInfo: nonExistentRecordInfo,
			RecordData: domain.RecordData{
				ShouldPayAddress: []domain.ExtendAddress{{Address: addr("PayX")}},
			},
		})
		assert.NoError(t, err)
		tripId, err := db.UpdateTripRecord(nonExistentRecordInfo.ID, cl)
		assert.Error(t, err)
		assert.Equal(t, uuid.Nil, tripId, "Trip ID should be nil for non-existent record")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestUpdateTripRecordRejectsInvalidAddressAtomically(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	trip := newTripInfo("Atomic update")
	otherTrip := newTripInfo("Other atomic update")
	assert.NoError(t, db.CreateTrip(trip))
	assert.NoError(t, db.CreateTrip(otherTrip))

	prePay, err := db.CreateAddress(trip.ID, "Update pre-pay")
	assert.NoError(t, err)
	shouldPay, err := db.CreateAddress(trip.ID, "Update should-pay")
	assert.NoError(t, err)
	foreign, err := db.CreateAddress(otherTrip.ID, "Foreign update address")
	assert.NoError(t, err)

	record := newRecord(
		"Atomic record",
		10,
		*prePay,
		[]domain.ExtendAddress{{Address: *shouldPay, ExtendMsg: 3}},
	)
	assert.NoError(t, db.CreateTripRecords(trip.ID, []domain.Record{record}))

	updated := record
	updated.ShouldPayAddress = []domain.ExtendAddress{{Address: *foreign, ExtendMsg: 7}}
	changeLog, err := diff.GetCustomDiffer().Diff(record, updated)
	assert.NoError(t, err)
	_, err = db.UpdateTripRecord(record.ID, changeLog)
	assert.Error(t, err)

	addresses, getErr := db.GetRecordAddressList(record.ID)
	assert.NoError(t, getErr)
	assert.Equal(
		t,
		[]domain.ExtendAddress{{Address: *shouldPay, ExtendMsg: 3}},
		addresses,
		"rejected update must not mutate the stored slice",
	)
}

func TestAddressCreateUpdate(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Kappa")
	_ = db.CreateTrip(tripInfo)

	t.Run("Successfully add address to list", func(t *testing.T) {
		alpha, err := db.CreateAddress(tripInfo.ID, "Address Alpha")
		assert.NoError(t, err)
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.Contains(t, list, *alpha)
		assert.Len(t, list, 1)

		beta, err := db.CreateAddress(tripInfo.ID, "Address Beta")
		assert.NoError(t, err)
		list, _ = db.GetTripAddressList(tripInfo.ID)
		assert.Contains(t, list, *beta)
		assert.Len(t, list, 2)
	})

	t.Run("Fail to add existing address", func(t *testing.T) {
		_, err := db.CreateAddress(tripInfo.ID, "Address Alpha")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.Len(t, list, 2) // Should still be 2
	})

	t.Run("Rename keeps identity and updates record views", func(t *testing.T) {
		address, err := db.CreateAddress(tripInfo.ID, "Address Gamma")
		assert.NoError(t, err)
		record := newRecord("Rename record", 10, *address, []domain.ExtendAddress{{Address: *address}})
		assert.NoError(t, db.CreateTripRecords(tripInfo.ID, []domain.Record{record}))

		updated, err := db.UpdateAddress(tripInfo.ID, address.ID, "Address Renamed")
		assert.NoError(t, err)
		assert.Equal(t, address.ID, updated.ID)
		records, err := db.GetTripRecords(tripInfo.ID)
		assert.NoError(t, err)
		assert.Equal(t, "Address Renamed", records[len(records)-1].PrePayAddress.Name)
		_, err = db.DeleteAddress(tripInfo.ID, address.ID)
		assert.Error(t, err)
	})

	t.Run("Reject address identity from another trip", func(t *testing.T) {
		otherTrip := newTripInfo("Other Trip")
		assert.NoError(t, db.CreateTrip(otherTrip))
		foreignAddress, err := db.CreateAddress(otherTrip.ID, "Foreign")
		assert.NoError(t, err)
		record := newRecord("Cross-trip", 10, *foreignAddress, []domain.ExtendAddress{{Address: *foreignAddress}})
		assert.Error(t, db.CreateTripRecords(tripInfo.ID, []domain.Record{record}))
	})

	t.Run("Fail to add address to non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := db.CreateAddress(nonExistentID, "Address Gamma")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestDeleteAddress(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	tripInfo := newTripInfo("Trip Lambda")
	_ = db.CreateTrip(tripInfo)
	addressX, _ := db.CreateAddress(tripInfo.ID, "Address X")
	addressY, _ := db.CreateAddress(tripInfo.ID, "Address Y")
	addressZ, _ := db.CreateAddress(tripInfo.ID, "Address Z")

	t.Run("Successfully remove address from list", func(t *testing.T) {
		_, err := db.DeleteAddress(tripInfo.ID, addressY.ID)
		assert.NoError(t, err)
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.NotContains(t, list, *addressY)
		assert.Len(t, list, 2)

		_, err = db.DeleteAddress(tripInfo.ID, addressX.ID)
		assert.NoError(t, err)
		list, _ = db.GetTripAddressList(tripInfo.ID)
		assert.NotContains(t, list, *addressX)
		assert.Len(t, list, 1)
	})

	t.Run("Fail to remove non-existent address", func(t *testing.T) {
		_, err := db.DeleteAddress(tripInfo.ID, uuid.New())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		list, _ := db.GetTripAddressList(tripInfo.ID)
		assert.Len(t, list, 1) // Should still be 1 (Address Z)
	})

	t.Run("Fail to remove address from non-existent trip", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := db.DeleteAddress(nonExistentID, addressZ.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestDeleteTrip(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	trip1 := newTripInfo("Trip Mu")
	_ = db.CreateTrip(trip1)
	record1 := newRecord("Rec Mu 1", 10.0, addr("P1"), []domain.ExtendAddress{{Address: addr("S1")}})
	_ = createRecords(t, db, trip1.ID, []domain.Record{record1})
	_, _ = db.CreateAddress(trip1.ID, "AddrM1")

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

	record1 := newRecord("Rec Xi 1", 10.0, addr("P1"), []domain.ExtendAddress{{Address: addr("S1")}})
	record2 := newRecord("Rec Xi 2", 20.0, addr("P2"), []domain.ExtendAddress{{Address: addr("S2")}})
	record3 := newRecord("Rec Xi 3", 30.0, addr("P3"), []domain.ExtendAddress{{Address: addr("S3")}})
	_ = createRecords(t, db, tripInfo.ID, []domain.Record{record1, record2, record3})

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
	rec1 := newRecord("Rec Omi 1", 1.0, addr("P1"), nil)
	rec2 := newRecord("Rec Omi 2", 2.0, addr("P2"), nil)
	_ = createRecords(t, db, trip1.ID, []domain.Record{rec1, rec2})

	trip2 := newTripInfo("Trip Pi")
	_ = db.CreateTrip(trip2)
	rec3 := newRecord("Rec Pi 1", 3.0, addr("P3"), nil)
	_ = createRecords(t, db, trip2.ID, []domain.Record{rec3})

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
		assert.Equal(t, result[nonExistentID], []domain.RecordInfo{}) // Missing key should have nil value
		assert.Contains(t, err.Error(), nonExistentID.String()+" not found")
	})
}

func TestDataLoaderGetTripAddressList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	ctx := context.Background()

	trip1 := newTripInfo("Trip Rho")
	_ = db.CreateTrip(trip1)
	a1, _ := db.CreateAddress(trip1.ID, "A1")
	a2, _ := db.CreateAddress(trip1.ID, "A2")

	trip2 := newTripInfo("Trip Sigma")
	_ = db.CreateTrip(trip2)
	// No addresses for trip2

	t.Run("Successfully load existing trip address lists", func(t *testing.T) {
		keys := []uuid.UUID{trip1.ID, trip2.ID}
		result, _ := db.DataLoaderGetTripAddressList(ctx, keys)
		assert.Len(t, result, 2)

		assert.Contains(t, result, trip1.ID)
		assert.ElementsMatch(t, []domain.Address{*a1, *a2}, result[trip1.ID])

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
		assert.ElementsMatch(t, []domain.Address{*a1, *a2}, result[trip1.ID])

		assert.Contains(t, result, nonExistentID)
		assert.Equal(t, result[nonExistentID], []domain.Address{}) // Missing key should have empty slice
		assert.Contains(t, err.Error(), nonExistentID.String()+" not found")
	})
}

func TestDataLoaderGetRecordShouldPayList(t *testing.T) {
	db := NewInMemoryTripDBWrapper()
	ctx := context.Background()

	trip1 := newTripInfo("Trip Tau")
	_ = db.CreateTrip(trip1)
	rec1 := newRecord("Rec Tau 1", 100.0, addr("P1"), []domain.ExtendAddress{
		{Address: addr("SP1"), ExtendMsg: 0.5},
		{Address: addr("SP2"), ExtendMsg: 1.0},
	})
	rec2 := newRecord("Rec Tau 2", 200.0, addr("P2"), []domain.ExtendAddress{
		{Address: addr("SP3"), ExtendMsg: 2.0},
	})
	rec3 := newRecord("Rec Tau 3", 300.0, addr("P3"), nil) // No should pay addresses
	_ = createRecords(t, db, trip1.ID, []domain.Record{rec1, rec2, rec3})

	t.Run("Successfully load existing record should pay lists", func(t *testing.T) {
		keys := []uuid.UUID{rec1.ID, rec2.ID, rec3.ID}
		result, _ := db.DataLoaderGetRecordShouldPayList(ctx, keys)
		// assert.NoError(t, err)
		assert.Len(t, result, 3)

		assert.Contains(t, result, rec1.ID)
		assert.ElementsMatch(t, []domain.ExtendAddress{
			{Address: addr("SP1"), ExtendMsg: 0.5},
			{Address: addr("SP2"), ExtendMsg: 1.0},
		}, result[rec1.ID])
		assert.Contains(t, result, rec2.ID)
		assert.ElementsMatch(t, []domain.ExtendAddress{
			{Address: addr("SP3"), ExtendMsg: 2.0},
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
		assert.ElementsMatch(t, []domain.ExtendAddress{
			{Address: addr("SP1"), ExtendMsg: 0.5},
			{Address: addr("SP2"), ExtendMsg: 1.0},
		}, result[rec1.ID])

		assert.Contains(t, result, nonExistentID)
		assert.Equal(t, result[nonExistentID], []domain.ExtendAddress{}) // Missing key should have empty slice
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
