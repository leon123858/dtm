package pg

import (
	"context"
	"dtm/db/db"
	"dtm/domain"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"dtm/libs/diff"
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
		err := gormDB.Exec("TRUNCATE TABLE record_should_pay_address_lists, records, addresses, trips RESTART IDENTITY CASCADE").Error
		if err != nil {
			// Fallback if TRUNCATE CASCADE isn't working as expected or not fully supported for all constraints.
			// This is a less ideal cleanup as it doesn't reset sequences typically.
			t.Logf("TRUNCATE CASCADE failed: %v. Attempting individual deletes.", err)
			gormDB.Exec("DELETE FROM record_should_pay_address_lists")
			gormDB.Exec("DELETE FROM records")
			gormDB.Exec("DELETE FROM addresses")
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

func mustCreateAddress(t *testing.T, wrapper db.TripDBWrapper, tripID uuid.UUID, name string) domain.Address {
	t.Helper()
	address, err := wrapper.CreateAddress(tripID, name)
	require.NoError(t, err)
	return *address
}

// --- Test Cases ---

func TestCreateTrip(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	tripInfo := &domain.TripInfo{
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
	tripInfo := &domain.TripInfo{ID: tripID, Name: "Trip For Records"}
	err := wrapper.CreateTrip(tripInfo)
	require.NoError(t, err)

	// Prerequisites for foreign keys in RecordModel and RecordShouldPayAddressListModel:
	// Addresses used in PrePayAddress and ShouldPayAddress must exist in TripAddressListModel for the trip.
	prePayAddr1 := mustCreateAddress(t, wrapper, tripID, "prepay_addr_for_records")
	shouldPayAddrA := mustCreateAddress(t, wrapper, tripID, "should_pay_A_for_records")
	shouldPayAddrB := mustCreateAddress(t, wrapper, tripID, "should_pay_B_for_records")
	shouldPayAddrC := mustCreateAddress(t, wrapper, tripID, "should_pay_C_for_records")

	recordID1 := uuid.New()
	recordID2 := uuid.New()
	time1 := time.Now()
	time2 := time.Now().Add(time.Hour)
	recordsToCreate := []domain.Record{
		{
			RecordInfo: domain.RecordInfo{
				ID:            recordID1,
				Name:          "Record 1",
				Amount:        100.50,
				PrePayAddress: prePayAddr1,
				Time:          time1,
				Category:      domain.CategoryFix,
			},
			RecordData: domain.RecordData{
				ShouldPayAddress: []domain.ExtendAddress{
					{Address: shouldPayAddrA, ExtendMsg: 20.0},
					{Address: shouldPayAddrB, ExtendMsg: 30.0},
				},
			},
		},
		{
			RecordInfo: domain.RecordInfo{
				ID:            recordID2,
				Name:          "Record 2",
				Amount:        200.75,
				PrePayAddress: prePayAddr1,
				Time:          time2,
				Category:      domain.CategoryNormal,
			},
			RecordData: domain.RecordData{
				ShouldPayAddress: []domain.ExtendAddress{
					{Address: shouldPayAddrC, ExtendMsg: 50.0},
				},
			},
		},
	}

	err = wrapper.CreateTripRecords(tripID, recordsToCreate)
	require.NoError(t, err)
	recordTripID, err := wrapper.GetRecordTripID(recordID1)
	require.NoError(t, err)
	assert.Equal(t, tripID, recordTripID)

	fetchedRecords, err := wrapper.GetTripRecords(tripID)
	require.NoError(t, err)
	require.Len(t, fetchedRecords, 2)

	// Sort records by name for consistent checking if order isn't guaranteed
	// For simplicity, we assume they are returned in creation order or test both possibilities.
	var r1, r2 domain.RecordInfo
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
	assert.Equal(t, domain.CategoryFix, r1.Category)
	shouldPay1, err := wrapper.GetRecordAddressList(recordID1)
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.ExtendAddress{
		{Address: shouldPayAddrA, ExtendMsg: 20.0},
		{Address: shouldPayAddrB, ExtendMsg: 30.0},
	}, shouldPay1)

	assert.Equal(t, recordID2, r2.ID)
	assert.Equal(t, "Record 2", r2.Name)
	assert.Equal(t, 200.75, r2.Amount)
	assert.Equal(t, prePayAddr1, r2.PrePayAddress)
	assert.Equal(t, time2.UnixMilli(), r2.Time.UnixMilli())
	assert.Equal(t, domain.CategoryNormal, r2.Category)
	shouldPay2, err := wrapper.GetRecordAddressList(recordID2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.ExtendAddress{
		{Address: shouldPayAddrC, ExtendMsg: 50.0},
	}, shouldPay2)
}

func TestGetTripRecords_NoRecords(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "Trip With No Records"})
	require.NoError(t, err)

	records, err := wrapper.GetTripRecords(tripID)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestCreateAddressAndGet(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "Trip For Address List"})
	require.NoError(t, err)

	addr1 := mustCreateAddress(t, wrapper, tripID, "addr1_test_talag")
	addr2 := mustCreateAddress(t, wrapper, tripID, "addr2_test_talag")
	_, err = wrapper.CreateAddress(tripID, addr1.Name)
	require.Error(t, err)

	addresses, err := wrapper.GetTripAddressList(tripID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.Address{addr1, addr2}, addresses)

	renamed, err := wrapper.UpdateAddress(tripID, addr1.ID, "addr1_renamed_talag")
	require.NoError(t, err)
	assert.Equal(t, addr1.ID, renamed.ID)
	addresses, err = wrapper.GetTripAddressList(tripID)
	require.NoError(t, err)
	assert.Contains(t, addresses, *renamed)
}

func TestDeleteAddress(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "Trip For Address Removal"})
	require.NoError(t, err)

	addr1 := mustCreateAddress(t, wrapper, tripID, "addr_to_remove1_talr")
	addr2 := mustCreateAddress(t, wrapper, tripID, "addr_to_keep_talr")

	_, err = wrapper.DeleteAddress(tripID, addr1.ID)
	require.NoError(t, err)

	addresses, err := wrapper.GetTripAddressList(tripID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.Address{addr2}, addresses)

	_, err = wrapper.DeleteAddress(tripID, uuid.New())
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestDeleteAddressWithRestrict(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripId := uuid.New()
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripId, Name: "Trip For Address Removal With Restrict"})
	require.NoError(t, err)
	addr1 := mustCreateAddress(t, wrapper, tripId, "addr1")
	addr2 := mustCreateAddress(t, wrapper, tripId, "addr2")
	addr3 := mustCreateAddress(t, wrapper, tripId, "addr3")
	addr4 := mustCreateAddress(t, wrapper, tripId, "addr4")

	// should not create record with not exist address
	wrongRecord := []domain.Record{
		{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "Sample Record", Amount: 50.0, PrePayAddress: addr1, Category: domain.CategoryNormal}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: domain.Address{ID: uuid.New(), Name: "addr_not_exist"}, ExtendMsg: 0.0}, {Address: addr2, ExtendMsg: 0.0}}}},
	}
	err = wrapper.CreateTripRecords(tripId, wrongRecord)
	require.Error(t, err)

	sampleRecord := []domain.Record{
		{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "Sample Record", Amount: 50.0, PrePayAddress: addr1, Category: domain.CategoryNormal}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: addr1, ExtendMsg: 0.0}, {Address: addr2, ExtendMsg: 0.0}}}},
	}
	err = wrapper.CreateTripRecords(tripId, sampleRecord)
	require.NoError(t, err)

	// should not rm address be records' owner
	_, err = wrapper.DeleteAddress(tripId, addr1.ID)
	require.Error(t, err)

	// should not rm address in records' pay list
	_, err = wrapper.DeleteAddress(tripId, addr2.ID)
	require.Error(t, err)

	// rm addr2 in pay list
	originalRecord := sampleRecord[0]
	sampleRecord[0].RecordData.ShouldPayAddress = []domain.ExtendAddress{{Address: addr1, ExtendMsg: 0.0}, {Address: addr3, ExtendMsg: 0.0}, {Address: addr4, ExtendMsg: 0.0}}
	cl, err := diff.GetCustomDiffer().Diff(originalRecord, sampleRecord[0])
	require.NoError(t, err)
	_, err = wrapper.UpdateTripRecord(sampleRecord[0].ID, cl)
	require.NoError(t, err)

	// can rm addr2
	_, err = wrapper.DeleteAddress(tripId, addr2.ID)
	require.NoError(t, err)

	// delete record
	_, err = wrapper.DeleteTripRecord(sampleRecord[0].ID)
	require.NoError(t, err)

	// can rm owner addr1
	_, err = wrapper.DeleteAddress(tripId, addr1.ID)
	require.NoError(t, err)
}

func TestUpdateTripInfo(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "Original Trip Name"})
	require.NoError(t, err)

	updatedInfo := &domain.TripInfo{ID: tripID, Name: "Updated Trip Name"}
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
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "Trip for Record Update"})
	require.NoError(t, err)

	prePayAddr := mustCreateAddress(t, wrapper, tripID, "prepay_for_update_test_utr")

	recordID := uuid.New()
	originalRecord := []domain.Record{
		{RecordInfo: domain.RecordInfo{ID: recordID, Name: "Original Record", Amount: 50.0, PrePayAddress: prePayAddr}},
	}
	err = wrapper.CreateTripRecords(tripID, originalRecord)
	require.NoError(t, err)

	curTime := time.Now()
	updatedRecordInfo := domain.RecordInfo{ID: recordID, Name: "Updated Record", Amount: 75.25, PrePayAddress: prePayAddr, Time: curTime}
	updatedRecord := domain.Record{
		RecordInfo: updatedRecordInfo,
		RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{
			{Address: domain.Address{ID: uuid.New(), Name: "shouldpay_for_update_test_utr"}, ExtendMsg: 10.0},
		}},
	}
	// should err as db constrain
	cl, err := diff.GetCustomDiffer().Diff(domain.Record{}, updatedRecord)
	require.NoError(t, err)
	tripId, err := wrapper.UpdateTripRecord(updatedRecordInfo.ID, cl)
	require.Error(t, err)
	assert.Empty(t, tripId)
	// should success as insert address
	shouldPayAddr := mustCreateAddress(t, wrapper, tripID, "shouldpay_for_update_test_utr")
	updatedRecord.ShouldPayAddress[0].Address = shouldPayAddr
	cl, err = diff.GetCustomDiffer().Diff(domain.Record{}, updatedRecord)
	require.NoError(t, err)
	tripId, err = wrapper.UpdateTripRecord(updatedRecordInfo.ID, cl)
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
	assert.ElementsMatch(t, []domain.ExtendAddress{
		{Address: shouldPayAddr, ExtendMsg: 10.0},
	}, shouldPayAddresses)
}

func TestDeleteTripRecord(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()

	tripID := uuid.New()
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "Trip for Record Deletion"})
	require.NoError(t, err)

	prePayAddr := mustCreateAddress(t, wrapper, tripID, "prepay_for_delete_dtr")
	shouldPayAddr := mustCreateAddress(t, wrapper, tripID, "shouldpay_for_delete_dtr")

	recordID := uuid.New()
	records := []domain.Record{
		{
			RecordInfo: domain.RecordInfo{ID: recordID, Name: "Record to Delete", Amount: 10, PrePayAddress: prePayAddr},
			RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{
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

	tripID := uuid.New()
	err := wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "Trip To Fully Delete"})
	require.NoError(t, err)

	addr := mustCreateAddress(t, wrapper, tripID, "addr_for_delete_trip_dt")

	recordID := uuid.New()
	records := []domain.Record{
		{
			RecordInfo: domain.RecordInfo{ID: recordID, Name: "Record in Deleted Trip", Amount: 1.0, PrePayAddress: addr},
			RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: addr, ExtendMsg: 0.5}}},
		},
	}
	err = wrapper.CreateTripRecords(tripID, records)
	require.NoError(t, err)

	err = wrapper.DeleteTrip(tripID)
	require.Error(t, err)

	// delete records
	_, err = wrapper.DeleteTripRecord(recordID)
	require.NoError(t, err)

	// delete addr
	_, err = wrapper.DeleteAddress(tripID, addr.ID)
	require.NoError(t, err)

	// delete trip success
	err = wrapper.DeleteTrip(tripID)
	require.NoError(t, err)
}

// --- Data Loader Tests ---

func TestDataLoaderGetTripInfoList(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	infos := []*domain.TripInfo{
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
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID1, Name: "DLRec Trip 1"}))
	tripID2 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID2, Name: "DLRec Trip 2"}))
	tripID3 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID3, Name: "DLRec Trip 3"})) // No records

	addrT1 := mustCreateAddress(t, wrapper, tripID1, "dlrec_t1_addr")
	addrT2 := mustCreateAddress(t, wrapper, tripID2, "dlrec_t2_addr")

	curTime := time.Now()
	rec1T1 := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "T1R1", PrePayAddress: addrT1, Time: curTime, Category: domain.CategoryFix}}
	rec2T1 := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "T1R2", PrePayAddress: addrT1, Time: curTime, Category: domain.CategoryNormal}}
	require.NoError(t, wrapper.CreateTripRecords(tripID1, []domain.Record{rec1T1, rec2T1}))

	rec1T2 := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "T2R1", PrePayAddress: addrT2, Time: curTime, Category: domain.CategoryNormal}}
	require.NoError(t, wrapper.CreateTripRecords(tripID2, []domain.Record{rec1T2}))

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
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID1, Name: "DLAddr Trip 1"}))
	tripID2 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID2, Name: "DLAddr Trip 2"}))
	tripID3 := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID3, Name: "DLAddr Trip 3"})) // No addresses

	addr1T1 := mustCreateAddress(t, wrapper, tripID1, "t1a1_dl")
	addr2T1 := mustCreateAddress(t, wrapper, tripID1, "t1a2_dl")

	addr1T2 := mustCreateAddress(t, wrapper, tripID2, "t2a1_dl")

	resultMap, err := wrapper.DataLoaderGetTripAddressList(ctx, []uuid.UUID{tripID1, tripID2, tripID3})
	require.NoError(t, err)
	require.Len(t, resultMap, 3)
	assert.ElementsMatch(t, []domain.Address{addr1T1, addr2T1}, resultMap[tripID1])
	assert.ElementsMatch(t, []domain.Address{addr1T2}, resultMap[tripID2])
	assert.Empty(t, resultMap[tripID3])
}

func TestDataLoaderGetRecordShouldPayList(t *testing.T) {
	wrapper, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tripID := uuid.New()
	require.NoError(t, wrapper.CreateTrip(&domain.TripInfo{ID: tripID, Name: "DLShouldPay Trip"}))

	// Pre-add all addresses to TripAddressList
	prePay := mustCreateAddress(t, wrapper, tripID, "dlsp_prepay")
	addrA := mustCreateAddress(t, wrapper, tripID, "dlsp_A")
	addrB := mustCreateAddress(t, wrapper, tripID, "dlsp_B")
	addrC := mustCreateAddress(t, wrapper, tripID, "dlsp_C")

	recID1 := uuid.New()
	recID2 := uuid.New()
	recID3 := uuid.New() // rec3 has no should pay
	recID4NonExistent := uuid.New()

	records := []domain.Record{
		{RecordInfo: domain.RecordInfo{ID: recID1, Name: "R1", PrePayAddress: prePay}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{
			{Address: addrA, ExtendMsg: 10.0},
			{Address: addrB, ExtendMsg: 20.0},
		}}},
		{RecordInfo: domain.RecordInfo{ID: recID2, Name: "R2", PrePayAddress: prePay}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{
			{Address: addrC, ExtendMsg: 50.0},
		}}},
		{RecordInfo: domain.RecordInfo{ID: recID3, Name: "R3", PrePayAddress: prePay}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{}}},
	}
	require.NoError(t, wrapper.CreateTripRecords(tripID, records))

	resultMap, err := wrapper.DataLoaderGetRecordShouldPayList(ctx, []uuid.UUID{recID1, recID2, recID3, recID4NonExistent})
	require.NoError(t, err)
	require.Len(t, resultMap, 4)
	assert.ElementsMatch(t, []domain.ExtendAddress{
		{Address: addrA, ExtendMsg: 10.0},
		{Address: addrB, ExtendMsg: 20.0},
	}, resultMap[recID1])
	assert.ElementsMatch(t, []domain.ExtendAddress{
		{Address: addrC, ExtendMsg: 50.0},
	}, resultMap[recID2])
	assert.Empty(t, resultMap[recID3])
	assert.Empty(t, resultMap[recID4NonExistent])
}
