package pg

import (
	"context"
	"dtm/db/db"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Helper function to get a test DSN.
// Prioritizes TEST_DATABASE_URL environment variable if set.
func getTestDSN() string {
	if testDSN := os.Getenv("TEST_DATABASE_URL"); testDSN != "" {
		return testDSN
	}
	// Fallback to the default DSN creator from init.go
	// IMPORTANT: Ensure this DSN points to a TEST database.
	return CreateDSN()
}

// setupTestDB initializes the database for testing and returns the wrapper and a cleanup function.
func setupTestDB(t *testing.T) (db.TripDBWrapper, func()) {
	dsn := getTestDSN()
	gormDB, err := InitPostgresGORM(dsn) // Assumes InitPostgresGORM handles base migrations from init.go
	require.NoError(t, err, "Failed to initialize test database using DSN: %s", dsn)

	tripDBWrapper := NewPgDBWrapper(gormDB)

	cleanup := func() {
		// Truncate tables to clean up data. Order matters if not using CASCADE effectively.
		// Using Exec for raw SQL.
		// RESTART IDENTITY is important to reset auto-incrementing PKs for predictable test data.
		// CASCADE should handle dependent rows.
		err := gormDB.Exec("TRUNCATE TABLE record_should_pay_address_lists, records, trip_address_lists, trips RESTART IDENTITY CASCADE").Error
		if err != nil {
			// Fallback if TRUNCATE CASCADE isn't working as expected or not fully supported for all constraints.
			// This is a less ideal cleanup as it doesn't reset sequences typically.
			t.Logf("TRUNCATE CASCADE failed: %v. Attempting individual deletes.", err)
			gormDB.Exec("DELETE FROM record_should_pay_address_lists")
			gormDB.Exec("DELETE FROM records")
			gormDB.Exec("DELETE FROM trip_address_lists")
			gormDB.Exec("DELETE FROM trips")
		}

		sqlDB, _ := gormDB.DB()
		err = sqlDB.Close()
		if err != nil {
			t.Logf("Error closing test DB connection: %v", err)
		}
	}

	return tripDBWrapper, cleanup
}

// --- Test Cases ---

func TestCreateTrip(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	tripInfo := &db.TripInfo{
		ID:   tripID,
		Name: "My Test Trip",
	}

	err := wrapper.CreateTrip(tripInfo)
	require.NoError(t, err)

	fetchedTrip, err := wrapper.GetTripInfo(tripID)
	require.NoError(t, err)
	require.NotNil(t, fetchedTrip)
	assert.Equal(t, tripInfo.ID, fetchedTrip.ID)
	assert.Equal(t, tripInfo.Name, fetchedTrip.Name)
}

func TestGetTripInfo_NotFound(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := wrapper.GetTripInfo(uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestCreateTripRecords(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	tripInfo := &db.TripInfo{ID: tripID, Name: "Trip For Records"}
	err := wrapper.CreateTrip(tripInfo)
	require.NoError(t, err)

	// Prerequisites for foreign keys in RecordModel and RecordShouldPayAddressListModel:
	// Addresses used in PrePayAddress and ShouldPayAddress must exist in TripAddressListModel for the trip.
	prePayAddr1 := db.Address("prepay_addr_for_records")
	shouldPayAddrA := db.Address("should_pay_A_for_records")
	shouldPayAddrB := db.Address("should_pay_B_for_records")
	shouldPayAddrC := db.Address("should_pay_C_for_records")

	require.NoError(t, wrapper.TripAddressListAdd(tripID, prePayAddr1))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, shouldPayAddrA))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, shouldPayAddrB))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, shouldPayAddrC))

	recordID1 := uuid.New()
	recordID2 := uuid.New()
	time1 := time.Now()
	time2 := time.Now().Add(time.Hour)
	recordsToCreate := []db.Record{
		{
			RecordInfo: db.RecordInfo{
				ID:            recordID1,
				Name:          "Record 1",
				Amount:        100.50,
				PrePayAddress: prePayAddr1,
				Time:          time1,
				Category:      db.CategoryFix,
			},
			RecordData: db.RecordData{
				ShouldPayAddress: []db.ExtendAddress{
					{Address: shouldPayAddrA, ExtendMsg: 20.0},
					{Address: shouldPayAddrB, ExtendMsg: 30.0},
				},
			},
		},
		{
			RecordInfo: db.RecordInfo{
				ID:            recordID2,
				Name:          "Record 2",
				Amount:        200.75,
				PrePayAddress: prePayAddr1,
				Time:          time2,
				Category:      db.CategoryNormal,
			},
			RecordData: db.RecordData{
				ShouldPayAddress: []db.ExtendAddress{
					{Address: shouldPayAddrC, ExtendMsg: 50.0},
				},
			},
		},
	}

	err = wrapper.CreateTripRecords(tripID, recordsToCreate)
	require.NoError(t, err)

	fetchedRecords, err := wrapper.GetTripRecords(tripID)
	require.NoError(t, err)
	require.Len(t, fetchedRecords, 2)

	// Sort records by name for consistent checking if order isn't guaranteed
	// For simplicity, we assume they are returned in creation order or test both possibilities.
	var r1, r2 db.RecordInfo
	if fetchedRecords[0].ID == recordID1 {
		r1, r2 = fetchedRecords[0], fetchedRecords[1]
	} else {
		r1, r2 = fetchedRecords[1], fetchedRecords[0]
	}

	assert.Equal(t, recordID1, r1.ID)
	assert.Equal(t, "Record 1", r1.Name)
	assert.Equal(t, 100.50, r1.Amount)
	assert.Equal(t, prePayAddr1, r1.PrePayAddress)
	assert.Equal(t, time1.UnixMilli(), r1.Time.UnixMilli())
	assert.Equal(t, db.CategoryFix, r1.Category)
	shouldPay1, err := wrapper.GetRecordAddressList(recordID1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []db.ExtendAddress{
		{Address: shouldPayAddrA, ExtendMsg: 20.0},
		{Address: shouldPayAddrB, ExtendMsg: 30.0},
	}, shouldPay1)

	assert.Equal(t, recordID2, r2.ID)
	assert.Equal(t, "Record 2", r2.Name)
	assert.Equal(t, 200.75, r2.Amount)
	assert.Equal(t, prePayAddr1, r2.PrePayAddress)
	assert.Equal(t, time2.UnixMilli(), r2.Time.UnixMilli())
	assert.Equal(t, db.CategoryNormal, r2.Category)
	shouldPay2, err := wrapper.GetRecordAddressList(recordID2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []db.ExtendAddress{
		{Address: shouldPayAddrC, ExtendMsg: 50.0},
	}, shouldPay2)
}

func TestGetTripRecords_NoRecords(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "Trip With No Records"})
	require.NoError(t, err)

	records, err := wrapper.GetTripRecords(tripID)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestTripAddressListAddAndGet(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "Trip For Address List"})
	require.NoError(t, err)

	addr1 := db.Address("addr1_test_talag")
	addr2 := db.Address("addr2_test_talag")

	require.NoError(t, wrapper.TripAddressListAdd(tripID, addr1))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, addr2))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, addr1)) // Test idempotency

	addresses, err := wrapper.GetTripAddressList(tripID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []db.Address{addr1, addr2}, addresses)
}

func TestTripAddressListRemove(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "Trip For Address Removal"})
	require.NoError(t, err)

	addr1 := db.Address("addr_to_remove1_talr")
	addr2 := db.Address("addr_to_keep_talr")

	require.NoError(t, wrapper.TripAddressListAdd(tripID, addr1))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, addr2))

	err = wrapper.TripAddressListRemove(tripID, addr1)
	require.NoError(t, err)

	addresses, err := wrapper.GetTripAddressList(tripID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []db.Address{addr2}, addresses)

	err = wrapper.TripAddressListRemove(tripID, db.Address("non_existent_addr_talr"))
	require.NoError(t, err) // Should not error
}

func TestUpdateTripInfo(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "Original Trip Name"})
	require.NoError(t, err)

	updatedInfo := &db.TripInfo{ID: tripID, Name: "Updated Trip Name"}
	err = wrapper.UpdateTripInfo(updatedInfo)
	require.NoError(t, err)

	fetchedTrip, err := wrapper.GetTripInfo(tripID)
	require.NoError(t, err)
	assert.Equal(t, updatedInfo.Name, fetchedTrip.Name)
}

func TestUpdateTripRecord(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "Trip for Record Update"})
	require.NoError(t, err)

	prePayAddr := db.Address("prepay_for_update_test_utr")
	require.NoError(t, wrapper.TripAddressListAdd(tripID, prePayAddr)) // Prereq for RecordModel FK

	recordID := uuid.New()
	originalRecord := []db.Record{
		{RecordInfo: db.RecordInfo{ID: recordID, Name: "Original Record", Amount: 50.0, PrePayAddress: prePayAddr}},
	}
	err = wrapper.CreateTripRecords(tripID, originalRecord)
	require.NoError(t, err)

	curTime := time.Now()
	updatedRecordInfo := db.RecordInfo{ID: recordID, Name: "Updated Record", Amount: 75.25, PrePayAddress: prePayAddr, Time: curTime}
	updatedRecord := db.Record{
		RecordInfo: updatedRecordInfo,
		RecordData: db.RecordData{ShouldPayAddress: []db.ExtendAddress{
			{Address: "shouldpay_for_update_test_utr", ExtendMsg: 10.0},
		}},
	}
	// should err as db constrain
	tripId, err := wrapper.UpdateTripRecord(&updatedRecord)
	require.Error(t, err)
	assert.Empty(t, tripId)
	// should success as insert address
	require.NoError(t, wrapper.TripAddressListAdd(tripID, db.Address("shouldpay_for_update_test_utr"))) // Add a should pay address
	tripId, err = wrapper.UpdateTripRecord(&updatedRecord)
	require.NoError(t, err)
	assert.Equal(t, tripID, tripId)

	fetchedRecords, err := wrapper.GetTripRecords(tripID)
	require.NoError(t, err)
	require.Len(t, fetchedRecords, 1)
	assert.Equal(t, updatedRecordInfo.Name, fetchedRecords[0].Name)
	assert.Equal(t, updatedRecordInfo.Amount, fetchedRecords[0].Amount)
	assert.Equal(t, updatedRecordInfo.PrePayAddress, fetchedRecords[0].PrePayAddress)

	// trip record time default is current
	assert.Equal(t, curTime.UnixMilli(), fetchedRecords[0].Time.UnixMilli())
	assert.NotEmpty(t, fetchedRecords[0].ID)

	shouldPayAddresses, err := wrapper.GetRecordAddressList(recordID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []db.ExtendAddress{
		{Address: "shouldpay_for_update_test_utr", ExtendMsg: 10.0},
	}, shouldPayAddresses)
}

func TestDeleteTripRecord(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "Trip for Record Deletion"})
	require.NoError(t, err)

	prePayAddr := db.Address("prepay_for_delete_dtr")
	shouldPayAddr := db.Address("shouldpay_for_delete_dtr")
	require.NoError(t, wrapper.TripAddressListAdd(tripID, prePayAddr))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, shouldPayAddr))

	recordID := uuid.New()
	records := []db.Record{
		{
			RecordInfo: db.RecordInfo{ID: recordID, Name: "Record to Delete", Amount: 10, PrePayAddress: prePayAddr},
			RecordData: db.RecordData{ShouldPayAddress: []db.ExtendAddress{
				{Address: shouldPayAddr, ExtendMsg: 5.0},
			}},
		},
	}
	err = wrapper.CreateTripRecords(tripID, records)
	require.NoError(t, err)

	tripId, err := wrapper.DeleteTripRecord(recordID)
	require.NoError(t, err)
	assert.Equal(t, tripID, tripId)

	fetchedRecords, err := wrapper.GetTripRecords(tripID)
	require.NoError(t, err)
	assert.Empty(t, fetchedRecords)

	// Verify associated RecordShouldPayAddressList entries are deleted (due to CASCADE)
	dbConn := (wrapper.(*pgDBWrapper)).db
	var count int64
	err = dbConn.Model(&RecordShouldPayAddressListModel{}).Where("record_id = ?", recordID).Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestDeleteTrip(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	dbConn := (wrapper.(*pgDBWrapper)).db // For direct DB checks

	tripID := uuid.New()
	err := wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "Trip To Fully Delete"})
	require.NoError(t, err)

	addr := db.Address("addr_for_delete_trip_dt")
	require.NoError(t, wrapper.TripAddressListAdd(tripID, addr))

	recordID := uuid.New()
	records := []db.Record{
		{
			RecordInfo: db.RecordInfo{ID: recordID, Name: "Record in Deleted Trip", Amount: 1.0, PrePayAddress: addr},
			RecordData: db.RecordData{ShouldPayAddress: []db.ExtendAddress{db.ExtendAddress{Address: addr, ExtendMsg: 0.5}}},
		},
	}
	err = wrapper.CreateTripRecords(tripID, records)
	require.NoError(t, err)

	err = wrapper.DeleteTrip(tripID)
	require.NoError(t, err)

	_, err = wrapper.GetTripInfo(tripID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var count int64
	err = dbConn.Model(&TripAddressListModel{}).Where("trip_id = ?", tripID).Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "TripAddressList entries should be deleted")

	err = dbConn.Model(&RecordModel{}).Where("trip_id = ?", tripID).Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "RecordModel entries should be deleted")

	err = dbConn.Model(&RecordShouldPayAddressListModel{}).Where("trip_id = ?", tripID).Count(&count).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "RecordShouldPayAddressListModel entries should be deleted")
}

// --- Data Loader Tests ---

func TestDataLoaderGetTripInfoList(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	infos := []*db.TripInfo{
		{ID: ids[0], Name: "DL Trip 1"},
		{ID: ids[1], Name: "DL Trip 2"},
	}
	require.NoError(t, wrapper.CreateTrip(infos[0]))
	require.NoError(t, wrapper.CreateTrip(infos[1]))

	resultMap, err := wrapper.DataLoaderGetTripInfoList(ctx, ids)
	require.NoError(t, err)
	require.Len(t, resultMap, 3)
	assert.Equal(t, infos[0].Name, resultMap[ids[0]].Name)
	assert.Equal(t, infos[1].Name, resultMap[ids[1]].Name)
	assert.Nil(t, resultMap[ids[2]])
}

func TestDataLoaderGetRecordInfoList(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tripID1 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&db.TripInfo{ID: tripID1, Name: "DLRec Trip 1"}))
	tripID2 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&db.TripInfo{ID: tripID2, Name: "DLRec Trip 2"}))
	tripID3 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&db.TripInfo{ID: tripID3, Name: "DLRec Trip 3"})) // No records

	addrT1 := db.Address("dlrec_t1_addr")
	require.NoError(t, wrapper.TripAddressListAdd(tripID1, addrT1))
	addrT2 := db.Address("dlrec_t2_addr")
	require.NoError(t, wrapper.TripAddressListAdd(tripID2, addrT2))

	curTime := time.Now()
	rec1T1 := db.Record{RecordInfo: db.RecordInfo{ID: uuid.New(), Name: "T1R1", PrePayAddress: addrT1, Time: curTime, Category: db.CategoryFix}}
	rec2T1 := db.Record{RecordInfo: db.RecordInfo{ID: uuid.New(), Name: "T1R2", PrePayAddress: addrT1, Time: curTime, Category: db.CategoryNormal}}
	require.NoError(t, wrapper.CreateTripRecords(tripID1, []db.Record{rec1T1, rec2T1}))

	rec1T2 := db.Record{RecordInfo: db.RecordInfo{ID: uuid.New(), Name: "T2R1", PrePayAddress: addrT2, Time: curTime, Category: db.CategoryNormal}}
	require.NoError(t, wrapper.CreateTripRecords(tripID2, []db.Record{rec1T2}))

	resultMap, err := wrapper.DataLoaderGetRecordInfoList(ctx, []uuid.UUID{tripID1, tripID2, tripID3})
	require.NoError(t, err)
	require.Len(t, resultMap, 3)
	assert.Len(t, resultMap[tripID1], 2)
	assert.Len(t, resultMap[tripID2], 1)
	assert.Empty(t, resultMap[tripID3])
}

func TestDataLoaderGetTripAddressList(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tripID1 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&db.TripInfo{ID: tripID1, Name: "DLAddr Trip 1"}))
	tripID2 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&db.TripInfo{ID: tripID2, Name: "DLAddr Trip 2"}))
	tripID3 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&db.TripInfo{ID: tripID3, Name: "DLAddr Trip 3"})) // No addresses

	addr1T1 := db.Address("t1a1_dl")
	addr2T1 := db.Address("t1a2_dl")
	require.NoError(t, wrapper.TripAddressListAdd(tripID1, addr1T1))
	require.NoError(t, wrapper.TripAddressListAdd(tripID1, addr2T1))

	addr1T2 := db.Address("t2a1_dl")
	require.NoError(t, wrapper.TripAddressListAdd(tripID2, addr1T2))

	resultMap, err := wrapper.DataLoaderGetTripAddressList(ctx, []uuid.UUID{tripID1, tripID2, tripID3})
	require.NoError(t, err)
	require.Len(t, resultMap, 3)
	assert.ElementsMatch(t, []db.Address{addr1T1, addr2T1}, resultMap[tripID1])
	assert.ElementsMatch(t, []db.Address{addr1T2}, resultMap[tripID2])
	assert.Empty(t, resultMap[tripID3])
}

func TestDataLoaderGetRecordShouldPayList(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tripID := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&db.TripInfo{ID: tripID, Name: "DLShouldPay Trip"}))

	// Pre-add all addresses to TripAddressList
	prePay := db.Address("dlsp_prepay")
	addrA := db.Address("dlsp_A")
	addrB := db.Address("dlsp_B")
	addrC := db.Address("dlsp_C")
	require.NoError(t, wrapper.TripAddressListAdd(tripID, prePay))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, addrA))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, addrB))
	require.NoError(t, wrapper.TripAddressListAdd(tripID, addrC))

	recID1 := uuid.New()
	recID2 := uuid.New()
	recID3 := uuid.New() // rec3 has no should pay
	recID4NonExistent := uuid.New()

	records := []db.Record{
		{RecordInfo: db.RecordInfo{ID: recID1, Name: "R1", PrePayAddress: prePay}, RecordData: db.RecordData{ShouldPayAddress: []db.ExtendAddress{
			{Address: addrA, ExtendMsg: 10.0},
			{Address: addrB, ExtendMsg: 20.0},
		}}},
		{RecordInfo: db.RecordInfo{ID: recID2, Name: "R2", PrePayAddress: prePay}, RecordData: db.RecordData{ShouldPayAddress: []db.ExtendAddress{
			{Address: addrC, ExtendMsg: 50.0},
		}}},
		{RecordInfo: db.RecordInfo{ID: recID3, Name: "R3", PrePayAddress: prePay}, RecordData: db.RecordData{ShouldPayAddress: []db.ExtendAddress{}}},
	}
	require.NoError(t, wrapper.CreateTripRecords(tripID, records))

	resultMap, err := wrapper.DataLoaderGetRecordShouldPayList(ctx, []uuid.UUID{recID1, recID2, recID3, recID4NonExistent})
	require.NoError(t, err)
	require.Len(t, resultMap, 4)
	assert.ElementsMatch(t, []db.ExtendAddress{
		{Address: addrA, ExtendMsg: 10.0},
		{Address: addrB, ExtendMsg: 20.0},
	}, resultMap[recID1])
	assert.ElementsMatch(t, []db.ExtendAddress{
		{Address: addrC, ExtendMsg: 50.0},
	}, resultMap[recID2])
	assert.Empty(t, resultMap[recID3])
	assert.Empty(t, resultMap[recID4NonExistent])
}
