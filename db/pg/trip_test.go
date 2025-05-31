package pg

import (
	// 為了確保可以在 TestMain 中關閉 *sql.DB

	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	dbt "dtm/db/db" // 假設這是您的 dbt 介面和結構的導入路徑
)

var testDB *gorm.DB
var tripDB dbt.TripDBWrapper

func initTest() {
	var err error
	testDB, err = InitPostgresGORM(CreateDSN())
	if err != nil {
		log.Fatalf("Failed to initialize test database: %v", err)
	}

	tripDB = NewGORMTripDBWrapper(testDB)
}

func cleanupTest() {
	log.Println("Cleaning up test database...")
	// 按照外鍵約束的順序刪除資料
	// 使用 Unscoped() 確保可以刪除所有記錄，即使有軟刪除設置 (GORM 軟刪除默認不會物理刪除)
	testDB.Exec("DELETE FROM trip_address_lists;")
	testDB.Exec("DELETE FROM records;")
	testDB.Exec("DELETE FROM trips;")
	log.Println("Test database cleaned.")
	CloseGORM(testDB)
}

func TestCreateTrip(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	tripInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Test Trip 1",
	}

	err := tripDB.CreateTrip(tripInfo)
	require.NoError(t, err, "CreateTrip should not return an error")

	// 驗證是否已建立
	retrievedInfo, err := tripDB.GetTripInfo(tripID)
	require.NoError(t, err, "GetTripInfo should not return an error after creation")
	assert.Equal(t, tripInfo.ID, retrievedInfo.ID)
	assert.Equal(t, tripInfo.Name, retrievedInfo.Name)

	// 測試重複建立
	err = tripDB.CreateTrip(tripInfo)
	assert.Error(t, err, "CreateTrip should return an error for duplicate ID")
	assert.True(t, strings.Contains(err.Error(), "already exists"), "Error message should indicate duplicate")
}

func TestCreateTripRecords(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Records"})
	require.NoError(t, err)

	recordID1 := uuid.New()
	recordID2 := uuid.New()
	records := []dbt.Record{
		{
			ID:            recordID1,
			Name:          "Record 1",
			Amount:        100.0,
			PrePayAddress: "AddrA",
			ShouldPayAddress: []dbt.Address{
				"Payer1", "Payer2",
			},
		},
		{
			ID:            recordID2,
			Name:          "Record 2",
			Amount:        200.50,
			PrePayAddress: "AddrB",
			ShouldPayAddress: []dbt.Address{
				"Payer3",
			},
		},
	}

	err = tripDB.CreateTripRecords(tripID, records)
	require.NoError(t, err, "CreateTripRecords should not return an error")

	// 驗證是否已建立
	retrievedRecords, err := tripDB.GetTripRecords(tripID)
	require.NoError(t, err, "GetTripRecords should not return an error")
	assert.Len(t, retrievedRecords, 2, "Should have 2 records")

	// 由於切片順序可能不同，按 ID 檢查
	foundCount := 0
	for _, r := range retrievedRecords {
		if r.ID == recordID1 {
			assert.Equal(t, records[0].Name, r.Name)
			assert.InDelta(t, records[0].Amount, r.Amount, 0.001) // 浮點數比較
			assert.Equal(t, records[0].PrePayAddress, r.PrePayAddress)
			assert.ElementsMatch(t, records[0].ShouldPayAddress, r.ShouldPayAddress)
			foundCount++
		} else if r.ID == recordID2 {
			assert.Equal(t, records[1].Name, r.Name)
			assert.InDelta(t, records[1].Amount, r.Amount, 0.001)
			assert.Equal(t, records[1].PrePayAddress, r.PrePayAddress)
			assert.ElementsMatch(t, records[1].ShouldPayAddress, r.ShouldPayAddress)
			foundCount++
		}
	}
	assert.Equal(t, 2, foundCount, "Both records should be found")

	// 測試為不存在的 Trip ID 建立 Records
	records = []dbt.Record{
		{
			ID:            uuid.New(),
			Name:          "Record 1",
			Amount:        100.0,
			PrePayAddress: "AddrA",
			ShouldPayAddress: []dbt.Address{
				"Payer1", "Payer2",
			},
		},
		{
			ID:            uuid.New(),
			Name:          "Record 2",
			Amount:        200.50,
			PrePayAddress: "AddrB",
			ShouldPayAddress: []dbt.Address{
				"Payer3",
			},
		},
	}

	nonExistentTripID := uuid.New()
	err = tripDB.CreateTripRecords(nonExistentTripID, records)
	assert.Error(t, err, "CreateTripRecords should return an error for non-existent trip ID")
	assert.True(t, strings.Contains(err.Error(), "not found"), "Error message should indicate trip not found")

	// 測試建立空 records slice
	err = tripDB.CreateTripRecords(tripID, []dbt.Record{})
	require.NoError(t, err, "Creating empty records should not return an error")
}

func TestGetTripInfo(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	tripInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Get Info Trip",
	}
	err := tripDB.CreateTrip(tripInfo)
	require.NoError(t, err)

	retrievedInfo, err := tripDB.GetTripInfo(tripID)
	require.NoError(t, err, "GetTripInfo should not return an error")
	assert.Equal(t, tripInfo.ID, retrievedInfo.ID)
	assert.Equal(t, tripInfo.Name, retrievedInfo.Name)

	// 測試獲取不存在的 Trip Info
	nonExistentID := uuid.New()
	_, err = tripDB.GetTripInfo(nonExistentID)
	assert.Error(t, err, "GetTripInfo should return an error for non-existent ID")
	assert.True(t, strings.Contains(err.Error(), "not found"), "Error message should indicate not found")
}

func TestGetTripRecords(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Get Records"})
	require.NoError(t, err)

	recordID1 := uuid.New()
	recordID2 := uuid.New()
	records := []dbt.Record{
		{ID: recordID1, Name: "GR1", Amount: 1.1, PrePayAddress: "P1", ShouldPayAddress: []dbt.Address{"A1", "A2"}},
		{ID: recordID2, Name: "GR2", Amount: 2.2, PrePayAddress: "P2", ShouldPayAddress: []dbt.Address{"A3"}},
	}
	err = tripDB.CreateTripRecords(tripID, records)
	require.NoError(t, err)

	retrievedRecords, err := tripDB.GetTripRecords(tripID)
	require.NoError(t, err, "GetTripRecords should not return an error")
	assert.Len(t, retrievedRecords, 2, "Should retrieve 2 records")

	// 驗證內容
	foundCount := 0
	for _, r := range retrievedRecords {
		if r.ID == recordID1 {
			assert.Equal(t, records[0].Name, r.Name)
			assert.InDelta(t, records[0].Amount, r.Amount, 0.001)
			assert.Equal(t, records[0].PrePayAddress, r.PrePayAddress)
			assert.ElementsMatch(t, records[0].ShouldPayAddress, r.ShouldPayAddress)
			foundCount++
		} else if r.ID == recordID2 {
			assert.Equal(t, records[1].Name, r.Name)
			assert.InDelta(t, records[1].Amount, r.Amount, 0.001)
			assert.Equal(t, records[1].PrePayAddress, r.PrePayAddress)
			assert.ElementsMatch(t, records[1].ShouldPayAddress, r.ShouldPayAddress)
			foundCount++
		}
	}
	assert.Equal(t, 2, foundCount, "Both records should be found and match")

	// 測試獲取不存在的 Trip ID 的記錄
	nonExistentTripID := uuid.New()
	retrievedRecords, err = tripDB.GetTripRecords(nonExistentTripID)
	require.NoError(t, err, "GetTripRecords for non-existent trip should return empty slice, not error") // GORM Find doesn't return error for no records
	assert.Empty(t, retrievedRecords, "Should return an empty slice for non-existent trip with no records")
}

func TestGetTripAddressList(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Addresses"})
	require.NoError(t, err)

	addresses := []dbt.Address{"Addr1", "Addr2", "Addr3"}
	for _, addr := range addresses {
		err = tripDB.TripAddressListAdd(tripID, addr)
		require.NoError(t, err)
	}

	retrievedAddresses, err := tripDB.GetTripAddressList(tripID)
	require.NoError(t, err, "GetTripAddressList should not return an error")
	assert.ElementsMatch(t, addresses, retrievedAddresses, "Retrieved addresses should match")

	// 測試獲取不存在的 Trip ID 的地址列表
	nonExistentTripID := uuid.New()
	retrievedAddresses, err = tripDB.GetTripAddressList(nonExistentTripID)
	require.NoError(t, err, "GetTripAddressList for non-existent trip should return empty slice, not error")
	assert.Empty(t, retrievedAddresses, "Should return an empty slice for non-existent trip")
}

func TestGetRecordAddressList(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Rec Addr List"})
	require.NoError(t, err)

	recordID1 := uuid.New()
	recordID2 := uuid.New()
	records := []dbt.Record{
		{
			ID:            recordID1,
			Name:          "Rec 1",
			Amount:        10.0,
			PrePayAddress: "P1",
			ShouldPayAddress: []dbt.Address{
				"R1_Payer1", "R1_Payer2",
			},
		},
		{
			ID:            recordID2,
			Name:          "Rec 2",
			Amount:        20.0,
			PrePayAddress: "P2",
			ShouldPayAddress: []dbt.Address{
				"R2_Payer1",
			},
		},
	}
	err = tripDB.CreateTripRecords(tripID, records)
	require.NoError(t, err)

	// 獲取 Record 1 的地址列表
	list1, err := tripDB.GetRecordAddressList(recordID1)
	require.NoError(t, err, "GetRecordAddressList for record1 should not return an error")
	assert.ElementsMatch(t, []dbt.Address{"R1_Payer1", "R1_Payer2"}, list1)

	// 獲取 Record 2 的地址列表
	list2, err := tripDB.GetRecordAddressList(recordID2)
	require.NoError(t, err, "GetRecordAddressList for record2 should not return an error")
	assert.ElementsMatch(t, []dbt.Address{"R2_Payer1"}, list2)

	// 測試獲取不存在的 Record ID 的地址列表
	nonExistentRecordID := uuid.New()
	_, err = tripDB.GetRecordAddressList(nonExistentRecordID)
	assert.Error(t, err, "GetRecordAddressList should return an error for non-existent ID")
	assert.True(t, strings.Contains(err.Error(), "not found"), "Error message should indicate not found")
}

func TestUpdateTripInfo(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	originalInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Original Trip Name",
	}
	err := tripDB.CreateTrip(originalInfo)
	require.NoError(t, err)

	updatedInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "New Trip Name",
	}
	err = tripDB.UpdateTripInfo(updatedInfo)
	require.NoError(t, err, "UpdateTripInfo should not return an error")

	retrievedInfo, err := tripDB.GetTripInfo(tripID)
	require.NoError(t, err)
	assert.Equal(t, updatedInfo.Name, retrievedInfo.Name, "Trip name should be updated")

	// 測試更新不存在的 Trip Info
	nonExistentID := uuid.New()
	nonExistentInfo := &dbt.TripInfo{
		ID:   nonExistentID,
		Name: "Non Existent Update",
	}
	err = tripDB.UpdateTripInfo(nonExistentInfo)
	assert.Error(t, err, "UpdateTripInfo should return an error for non-existent ID")
	assert.True(t, strings.Contains(err.Error(), "not found for update"), "Error message should indicate not found for update")
}

func TestUpdateTripRecord(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Update Record"})
	require.NoError(t, err)

	recordID := uuid.New()
	originalRecord := dbt.Record{
		ID:            recordID,
		Name:          "Original Record",
		Amount:        99.99,
		PrePayAddress: "OrigP",
		ShouldPayAddress: []dbt.Address{
			"O1", "O2",
		},
	}
	err = tripDB.CreateTripRecords(tripID, []dbt.Record{originalRecord})
	require.NoError(t, err)

	updatedRecord := dbt.Record{
		ID:            recordID,
		Name:          "Updated Record Name",
		Amount:        199.99,
		PrePayAddress: "UpdatedP",
		ShouldPayAddress: []dbt.Address{
			"U1", "U2", "U3",
		},
	}
	err = tripDB.UpdateTripRecord(updatedRecord)
	require.NoError(t, err, "UpdateTripRecord should not return an error")

	// 驗證更新是否成功
	retrievedRecords, err := tripDB.GetTripRecords(tripID)
	require.NoError(t, err)
	assert.Len(t, retrievedRecords, 1, "Should still have 1 record")
	retrievedRecord := retrievedRecords[0]
	assert.Equal(t, updatedRecord.Name, retrievedRecord.Name)
	assert.InDelta(t, updatedRecord.Amount, retrievedRecord.Amount, 0.001)
	assert.Equal(t, updatedRecord.PrePayAddress, retrievedRecord.PrePayAddress)
	assert.ElementsMatch(t, updatedRecord.ShouldPayAddress, retrievedRecord.ShouldPayAddress)

	// 測試更新不存在的 Record
	nonExistentRecordID := uuid.New()
	nonExistentRecord := dbt.Record{
		ID:   nonExistentRecordID,
		Name: "Non Existent Record",
	}
	err = tripDB.UpdateTripRecord(nonExistentRecord)
	assert.Error(t, err, "UpdateTripRecord should return an error for non-existent record")
	assert.True(t, strings.Contains(err.Error(), "not found for update"), "Error message should indicate not found for update")
}

func TestTripAddressListAdd(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Addr List Add"})
	require.NoError(t, err)

	address1 := dbt.Address("add_addr1@example.com")
	err = tripDB.TripAddressListAdd(tripID, address1)
	require.NoError(t, err, "TripAddressListAdd should not return an error")

	retrievedList, err := tripDB.GetTripAddressList(tripID)
	require.NoError(t, err)
	assert.Contains(t, retrievedList, address1)
	assert.Len(t, retrievedList, 1)

	// 測試添加重複地址
	err = tripDB.TripAddressListAdd(tripID, address1)
	assert.Error(t, err, "TripAddressListAdd should return an error for duplicate address")
	assert.True(t, strings.Contains(err.Error(), "already exists"), "Error message should indicate already exists")

	// 測試為不存在的 Trip 添加地址
	nonExistentTripID := uuid.New()
	err = tripDB.TripAddressListAdd(nonExistentTripID, "orphan@example.com")
	assert.Error(t, err, "TripAddressListAdd should return an error for non-existent trip")
	assert.True(t, strings.Contains(err.Error(), "not found"), "Error message should indicate trip not found")
}

func TestTripAddressListRemove(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Addr List Remove"})
	require.NoError(t, err)

	address1 := dbt.Address("rem_addr1@example.com")
	address2 := dbt.Address("rem_addr2@example.com")
	err = tripDB.TripAddressListAdd(tripID, address1)
	require.NoError(t, err)
	err = tripDB.TripAddressListAdd(tripID, address2)
	require.NoError(t, err)

	// 移除地址1
	err = tripDB.TripAddressListRemove(tripID, address1)
	require.NoError(t, err, "TripAddressListRemove should not return an error")

	retrievedList, err := tripDB.GetTripAddressList(tripID)
	require.NoError(t, err)
	assert.NotContains(t, retrievedList, address1)
	assert.Contains(t, retrievedList, address2)
	assert.Len(t, retrievedList, 1)

	// 測試移除不存在的地址
	nonExistentAddress := dbt.Address("non_existent@example.com")
	err = tripDB.TripAddressListRemove(tripID, nonExistentAddress)
	assert.Error(t, err, "TripAddressListRemove should return an error for non-existent address")
	assert.True(t, strings.Contains(err.Error(), "not found"), "Error message should indicate not found")

	// 測試為不存在的 Trip 移除地址
	nonExistentTripID := uuid.New()
	err = tripDB.TripAddressListRemove(nonExistentTripID, address2)
	assert.Error(t, err, "TripAddressListRemove should return an error for non-existent trip")
	assert.True(t, strings.Contains(err.Error(), "not found"), "Error message should indicate not found")
}

func TestDeleteTrip(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip to Delete"})
	require.NoError(t, err)

	// 建立相關聯的記錄和地址，驗證 CASCADE DELETE
	recordID := uuid.New()
	err = tripDB.CreateTripRecords(tripID, []dbt.Record{
		{ID: recordID, Name: "Rec for Delete", Amount: 1.0, PrePayAddress: "P", ShouldPayAddress: []dbt.Address{"A"}},
	})
	require.NoError(t, err)
	err = tripDB.TripAddressListAdd(tripID, "addr_to_delete@example.com")
	require.NoError(t, err)

	err = tripDB.DeleteTrip(tripID)
	require.NoError(t, err, "DeleteTrip should not return an error")

	// 驗證 Trip 是否被刪除
	_, err = tripDB.GetTripInfo(tripID)
	assert.Error(t, err, "GetTripInfo should return an error after deletion")
	assert.True(t, strings.Contains(err.Error(), "not found"), "Error message should indicate not found")

	// 驗證相關聯的 Records 是否被刪除 (ON DELETE CASCADE)
	retrievedRecords, err := tripDB.GetTripRecords(tripID)
	require.NoError(t, err, "GetTripRecords for deleted trip should not error")
	assert.Empty(t, retrievedRecords, "Records for deleted trip should be empty")

	// 驗證相關聯的 AddressList 是否被刪除 (ON DELETE CASCADE)
	retrievedAddresses, err := tripDB.GetTripAddressList(tripID)
	require.NoError(t, err, "GetTripAddressList for deleted trip should not error")
	assert.Empty(t, retrievedAddresses, "AddressList for deleted trip should be empty")

	// 測試刪除不存在的 Trip
	nonExistentID := uuid.New()
	err = tripDB.DeleteTrip(nonExistentID)
	assert.Error(t, err, "DeleteTrip should return an error for non-existent ID")
	assert.True(t, strings.Contains(err.Error(), "not found for deletion"), "Error message should indicate not found for deletion")
}

func TestDeleteTripRecord(t *testing.T) {
	initTest()
	defer cleanupTest()

	tripID := uuid.New()
	err := tripDB.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Delete Record"})
	require.NoError(t, err)

	recordID1 := uuid.New()
	recordID2 := uuid.New()
	records := []dbt.Record{
		{ID: recordID1, Name: "Rec 1", Amount: 1.0, PrePayAddress: "P1", ShouldPayAddress: []dbt.Address{"A1"}},
		{ID: recordID2, Name: "Rec 2", Amount: 2.0, PrePayAddress: "P2", ShouldPayAddress: []dbt.Address{"A2"}},
	}
	err = tripDB.CreateTripRecords(tripID, records)
	require.NoError(t, err)

	// 刪除 Record 1
	err = tripDB.DeleteTripRecord(recordID1)
	require.NoError(t, err, "DeleteTripRecord should not return an error")

	// 驗證 Record 1 是否被刪除
	retrievedRecords, err := tripDB.GetTripRecords(tripID)
	require.NoError(t, err)
	assert.Len(t, retrievedRecords, 1, "Should have 1 record remaining")
	assert.Equal(t, recordID2, retrievedRecords[0].ID, "Remaining record should be Record 2")

	// 測試刪除不存在的 Record
	nonExistentRecordID := uuid.New()
	err = tripDB.DeleteTripRecord(nonExistentRecordID)
	assert.Error(t, err, "DeleteTripRecord should return an error for non-existent ID")
	assert.True(t, strings.Contains(err.Error(), "not found for deletion"), "Error message should indicate not found for deletion")
}

func TestDataLoaderGetRecordList(t *testing.T) {
	initTest()
	defer cleanupTest()

	// 2. Prepare test data
	tripID1 := uuid.New()
	tripID2 := uuid.New()

	// Insert TripInfoModels first since RecordModel has a foreign key to it
	testDB.Create(&TripInfoModel{ID: tripID1, Name: "Trip A"})
	testDB.Create(&TripInfoModel{ID: tripID2, Name: "Trip B"})

	record1ID := uuid.New()
	record2ID := uuid.New()
	record3ID := uuid.New() // This record will not be requested

	record1 := RecordModel{
		ID:               record1ID,
		TripID:           tripID1,
		Name:             "Expense A",
		Amount:           100.50,
		PrePayAddress:    "Addr A1",
		ShouldPayAddress: pq.StringArray{"Addr A2", "Addr A3"},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	record2 := RecordModel{
		ID:               record2ID,
		TripID:           tripID2,
		Name:             "Expense B",
		Amount:           250.75,
		PrePayAddress:    "Addr B1",
		ShouldPayAddress: pq.StringArray{"Addr B2"},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	record3 := RecordModel{
		ID:               record3ID,
		TripID:           tripID1,
		Name:             "Expense C",
		Amount:           50.00,
		PrePayAddress:    "Addr C1",
		ShouldPayAddress: pq.StringArray{},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	testDB.Create(&record1)
	testDB.Create(&record2)
	testDB.Create(&record3)

	t.Run("Successfully retrieves multiple records", func(t *testing.T) {
		keys := []uuid.UUID{record1ID, record2ID}
		recordsMap, errorsMap := tripDB.DataLoaderGetRecordList(context.Background(), keys)

		assert.Empty(t, errorsMap, "Expected no errors for successful retrieval")
		assert.Len(t, recordsMap, 2, "Expected 2 records to be returned")

		// Verify record1
		rec1, ok := recordsMap[record1ID]
		assert.True(t, ok, "Record 1 should be in the map")
		assert.Equal(t, record1.ID, rec1.ID)
		assert.Equal(t, record1.Name, rec1.Name)
		assert.InDelta(t, record1.Amount, rec1.Amount, 0.001) // Use InDelta for float comparison
		assert.Equal(t, dbt.Address(record1.PrePayAddress), rec1.PrePayAddress)
		assert.ElementsMatch(t, []dbt.Address{dbt.Address("Addr A2"), dbt.Address("Addr A3")}, rec1.ShouldPayAddress)

		// Verify record2
		rec2, ok := recordsMap[record2ID]
		assert.True(t, ok, "Record 2 should be in the map")
		assert.Equal(t, record2.ID, rec2.ID)
		assert.Equal(t, record2.Name, rec2.Name)
		assert.InDelta(t, record2.Amount, rec2.Amount, 0.001)
		assert.Equal(t, dbt.Address(record2.PrePayAddress), rec2.PrePayAddress)
		assert.ElementsMatch(t, []dbt.Address{dbt.Address("Addr B2")}, rec2.ShouldPayAddress)
	})

	t.Run("Handles missing keys", func(t *testing.T) {
		missingID := uuid.New() // A non-existent ID
		keys := []uuid.UUID{record1ID, missingID}
		recordsMap, errorsMap := tripDB.DataLoaderGetRecordList(context.Background(), keys)

		assert.Len(t, recordsMap, 1, "Expected only 1 record to be found")
		assert.Len(t, errorsMap, 1, "Expected 1 error for the missing key")

		// Verify the found record
		_, ok := recordsMap[record1ID]
		assert.True(t, ok, "Record 1 should be in the map")

		// Verify the error for the missing key
		errVal, ok := errorsMap[missingID]
		assert.True(t, ok, "Error for missing ID should be in the map")
		assert.Contains(t, errVal.Error(), missingID.String(), "Error message should contain the missing ID")
	})

	t.Run("Handles all keys missing", func(t *testing.T) {
		keys := []uuid.UUID{uuid.New(), uuid.New()} // Two non-existent IDs
		recordsMap, errorsMap := tripDB.DataLoaderGetRecordList(context.Background(), keys)

		assert.Empty(t, recordsMap, "Expected no records to be found")
		assert.Len(t, errorsMap, 2, "Expected 2 errors for missing keys")

		for _, key := range keys {
			errVal, ok := errorsMap[key]
			assert.True(t, ok, "Error should exist for all missing keys")
			assert.Contains(t, errVal.Error(), key.String(), "Error message should contain the missing ID")
		}
	})

	t.Run("Handles empty keys slice", func(t *testing.T) {
		keys := []uuid.UUID{}
		recordsMap, errorsMap := tripDB.DataLoaderGetRecordList(context.Background(), keys)

		assert.Empty(t, recordsMap, "Expected no records for empty keys slice")
		assert.Empty(t, errorsMap, "Expected no errors for empty keys slice")
	})
}
