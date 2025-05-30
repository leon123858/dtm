package mem_test // Use _test suffix for test package

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	dbt "dtm/db/db" // Assuming dtm/db/db contains the interface and types
	"dtm/db/mem"    // Import the package containing inMemoryTripDBWrapper
)

// setupTest creates a new inMemoryTripDBWrapper instance for each test.
func setupTest() dbt.TripDBWrapper {
	return mem.NewInMemoryTripDBWrapper()
}

func TestCreateTrip(t *testing.T) {
	db := setupTest()

	// Test 1: Successfully create a trip
	tripID := uuid.New()
	tripInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Test Trip 1",
	}
	err := db.CreateTrip(tripInfo)
	assert.NoError(t, err, "CreateTrip should not return an error for a new trip")

	retrievedInfo, err := db.GetTripInfo(tripID)
	assert.NoError(t, err)
	assert.NotNil(t, retrievedInfo)
	assert.Equal(t, tripInfo.ID, retrievedInfo.ID)
	assert.Equal(t, tripInfo.Name, retrievedInfo.Name)

	// Test 2: Try to create a trip with an existing ID (should fail)
	err = db.CreateTrip(tripInfo)
	assert.Error(t, err, "CreateTrip should return an error for a duplicate trip ID")
	assert.Contains(t, err.Error(), "already exists")
}

func TestGetTripInfo(t *testing.T) {
	db := setupTest()

	// Prepare data
	tripID := uuid.New()
	tripInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Test Trip Get Info",
	}
	db.CreateTrip(tripInfo)

	// Test 1: Get existing trip info
	retrievedInfo, err := db.GetTripInfo(tripID)
	assert.NoError(t, err)
	assert.NotNil(t, retrievedInfo)
	assert.Equal(t, tripInfo.ID, retrievedInfo.ID)
	assert.Equal(t, tripInfo.Name, retrievedInfo.Name)

	// Test 2: Get non-existent trip info (should fail)
	nonExistentID := uuid.New()
	retrievedInfo, err = db.GetTripInfo(nonExistentID)
	assert.Error(t, err, "GetTripInfo should return an error for non-existent trip")
	assert.Nil(t, retrievedInfo)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateTripInfo(t *testing.T) {
	db := setupTest()

	// Prepare data
	tripID := uuid.New()
	tripInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Original Name",
	}
	db.CreateTrip(tripInfo)

	// Test 1: Successfully update trip info
	updatedTripInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Updated Name",
	}
	err := db.UpdateTripInfo(updatedTripInfo)
	assert.NoError(t, err, "UpdateTripInfo should not return an error")

	retrievedInfo, err := db.GetTripInfo(tripID)
	assert.NoError(t, err)
	assert.NotNil(t, retrievedInfo)
	assert.Equal(t, updatedTripInfo.Name, retrievedInfo.Name) // Check if name is updated

	// Test 2: Try to update non-existent trip info (should fail)
	nonExistentID := uuid.New()
	nonExistentTripInfo := &dbt.TripInfo{
		ID:   nonExistentID,
		Name: "Non Existent",
	}
	err = db.UpdateTripInfo(nonExistentTripInfo)
	assert.Error(t, err, "UpdateTripInfo should return an error for non-existent trip")
	assert.Contains(t, err.Error(), "not found for update")
}

func TestDeleteTrip(t *testing.T) {
	db := setupTest()

	// Prepare data
	tripID := uuid.New()
	tripInfo := &dbt.TripInfo{
		ID:   tripID,
		Name: "Trip to Delete",
	}
	db.CreateTrip(tripInfo)

	// Test 1: Successfully delete a trip
	err := db.DeleteTrip(tripID)
	assert.NoError(t, err, "DeleteTrip should not return an error")

	retrievedInfo, err := db.GetTripInfo(tripID)
	assert.Error(t, err, "GetTripInfo should return an error after deletion")
	assert.Nil(t, retrievedInfo)
	assert.Contains(t, err.Error(), "not found")

	// Test 2: Try to delete a non-existent trip (should fail)
	nonExistentID := uuid.New()
	err = db.DeleteTrip(nonExistentID)
	assert.Error(t, err, "DeleteTrip should return an error for non-existent trip")
	assert.Contains(t, err.Error(), "not found for deletion")
}

func TestCreateTripRecords(t *testing.T) {
	db := setupTest()

	// Prepare trip
	tripID := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Records"})

	// Test 1: Successfully add records to an existing trip
	record1 := dbt.Record{ID: uuid.New(), Name: "Record A", Amount: 100.0}
	record2 := dbt.Record{ID: uuid.New(), Name: "Record B", Amount: 200.0}
	recordsToAdd := []dbt.Record{record1, record2}

	err := db.CreateTripRecords(tripID, recordsToAdd)
	assert.NoError(t, err, "CreateTripRecords should not return an error")

	retrievedRecords, err := db.GetTripRecords(tripID)
	assert.NoError(t, err)
	assert.Len(t, retrievedRecords, 2)
	assert.Contains(t, retrievedRecords, record1)
	assert.Contains(t, retrievedRecords, record2)

	// Test 2: Add more records
	record3 := dbt.Record{ID: uuid.New(), Name: "Record C", Amount: 300.0}
	err = db.CreateTripRecords(tripID, []dbt.Record{record3})
	assert.NoError(t, err)
	retrievedRecords, err = db.GetTripRecords(tripID)
	assert.NoError(t, err)
	assert.Len(t, retrievedRecords, 3)
	assert.Contains(t, retrievedRecords, record3)

	// Test 3: Try to add records to a non-existent trip (should fail)
	nonExistentID := uuid.New()
	err = db.CreateTripRecords(nonExistentID, recordsToAdd)
	assert.Error(t, err, "CreateTripRecords should return an error for non-existent trip")
	assert.Contains(t, err.Error(), "not found")
}

func TestGetTripRecords(t *testing.T) {
	db := setupTest()

	// Prepare trip and records
	tripID := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Get Records"})
	record1 := dbt.Record{ID: uuid.New(), Name: "Record X", Amount: 10.0}
	db.CreateTripRecords(tripID, []dbt.Record{record1})

	// Test 1: Get records for an existing trip
	retrievedRecords, err := db.GetTripRecords(tripID)
	assert.NoError(t, err)
	assert.Len(t, retrievedRecords, 1)
	assert.Equal(t, record1.ID, retrievedRecords[0].ID)

	// Test 2: Get records for a trip with no records
	tripIDNoRecords := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripIDNoRecords, Name: "Trip No Records"})
	retrievedRecords, err = db.GetTripRecords(tripIDNoRecords)
	assert.NoError(t, err)
	assert.Empty(t, retrievedRecords)

	// Test 3: Get records for a non-existent trip (should fail)
	nonExistentID := uuid.New()
	retrievedRecords, err = db.GetTripRecords(nonExistentID)
	assert.Error(t, err, "GetTripRecords should return an error for non-existent trip")
	assert.Nil(t, retrievedRecords)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetRecordAddressList(t *testing.T) {
	db := setupTest()

	// Prepare trip and records
	tripID := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Record Address List"})
	record1 := dbt.Record{ID: uuid.New(), Name: "Record 1", Amount: 50.0, PrePayAddress: "Address 1", ShouldPayAddress: []dbt.Address{"Address 1"}}
	record2 := dbt.Record{ID: uuid.New(), Name: "Record 2", Amount: 75.0, PrePayAddress: "Address 2", ShouldPayAddress: []dbt.Address{"Address 2"}}
	err := db.CreateTripRecords(tripID, []dbt.Record{record1, record2})
	assert.NoError(t, err, "CreateTripRecords should not return an error")

	// Test 1: Get address list for an existing record
	addressList, err := db.GetRecordAddressList(record1.ID)
	assert.NoError(t, err)
	assert.Len(t, addressList, 1)
	assert.Equal(t, record1.PrePayAddress, addressList[0])

	// Test 2: Get address list for a non-existent record (should fail)
	nonExistentRecordID := uuid.New()
	addressList, err = db.GetRecordAddressList(nonExistentRecordID)
	assert.Error(t, err, "GetRecordAddressList should return an error for non-existent record")
	assert.Nil(t, addressList)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateTripRecord(t *testing.T) {
	db := setupTest()

	// Prepare trip and records
	tripID := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Update Record"})
	record1 := dbt.Record{ID: uuid.New(), Name: "Old Name", Amount: 100.0}
	db.CreateTripRecords(tripID, []dbt.Record{record1})

	// Test 1: Successfully update an existing record
	updatedRecord := dbt.Record{ID: record1.ID, Name: "New Name", Amount: 150.0}
	err := db.UpdateTripRecord(updatedRecord)
	assert.NoError(t, err, "UpdateTripRecord should not return an error")

	retrievedRecords, err := db.GetTripRecords(tripID)
	assert.NoError(t, err)
	assert.Len(t, retrievedRecords, 1)
	assert.Equal(t, updatedRecord.Name, retrievedRecords[0].Name)
	assert.Equal(t, updatedRecord.Amount, retrievedRecords[0].Amount)

	// Test 2: Try to update a non-existent record (should fail)
	nonExistentRecord := dbt.Record{ID: uuid.New(), Name: "Fake", Amount: 999.9}
	err = db.UpdateTripRecord(nonExistentRecord)
	assert.Error(t, err, "UpdateTripRecord should return an error for non-existent record")
	assert.Contains(t, err.Error(), "not found for update")
}

func TestDeleteTripRecord(t *testing.T) {
	db := setupTest()

	// Prepare trip and records
	tripID := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Delete Record"})
	record1 := dbt.Record{ID: uuid.New(), Name: "Record 1", Amount: 100.0}
	record2 := dbt.Record{ID: uuid.New(), Name: "Record 2", Amount: 200.0}
	db.CreateTripRecords(tripID, []dbt.Record{record1, record2})

	// Test 1: Successfully delete an existing record
	err := db.DeleteTripRecord(record1.ID)
	assert.NoError(t, err, "DeleteTripRecord should not return an error")

	retrievedRecords, err := db.GetTripRecords(tripID)
	assert.NoError(t, err)
	assert.Len(t, retrievedRecords, 1)
	assert.Equal(t, record2.ID, retrievedRecords[0].ID) // Only record2 should remain

	// Test 2: Try to delete a non-existent record from an existing trip (should fail)
	nonExistentRecordID := uuid.New()
	err = db.DeleteTripRecord(nonExistentRecordID)
	assert.Error(t, err, "DeleteTripRecord should return an error for non-existent record")
	assert.Contains(t, err.Error(), "not found")
}

func TestTripAddressListAdd(t *testing.T) {
	db := setupTest()

	// Prepare trip
	tripID := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Addresses"})

	// Test 1: Successfully add addresses
	addr1 := dbt.Address("Address A")
	addr2 := dbt.Address("Address B")

	err := db.TripAddressListAdd(tripID, addr1)
	assert.NoError(t, err)
	err = db.TripAddressListAdd(tripID, addr2)
	assert.NoError(t, err)

	retrievedAddresses, err := db.GetTripAddressList(tripID)
	assert.NoError(t, err)
	assert.Len(t, retrievedAddresses, 2)
	assert.Contains(t, retrievedAddresses, addr1)
	assert.Contains(t, retrievedAddresses, addr2)

	// Test 2: Try to add a duplicate address (should fail)
	err = db.TripAddressListAdd(tripID, addr1)
	assert.Error(t, err, "TripAddressListAdd should return an error for duplicate address")
	assert.Contains(t, err.Error(), "already exists")
	retrievedAddresses, _ = db.GetTripAddressList(tripID)
	assert.Len(t, retrievedAddresses, 2) // Length should remain 2

	// Test 3: Try to add address to a non-existent trip (should fail)
	nonExistentID := uuid.New()
	err = db.TripAddressListAdd(nonExistentID, "Fake Address")
	assert.Error(t, err, "TripAddressListAdd should return an error for non-existent trip")
	assert.Contains(t, err.Error(), "not found")
}

func TestTripAddressListRemove(t *testing.T) {
	db := setupTest()

	// Prepare trip and addresses
	tripID := uuid.New()
	db.CreateTrip(&dbt.TripInfo{ID: tripID, Name: "Trip for Address Removal"})
	addr1 := dbt.Address("Address 1")
	addr2 := dbt.Address("Address 2")
	addr3 := dbt.Address("Address 3")
	db.TripAddressListAdd(tripID, addr1)
	db.TripAddressListAdd(tripID, addr2)
	db.TripAddressListAdd(tripID, addr3)

	// Test 1: Successfully remove an address
	err := db.TripAddressListRemove(tripID, addr2)
	assert.NoError(t, err)

	retrievedAddresses, err := db.GetTripAddressList(tripID)
	assert.NoError(t, err)
	assert.Len(t, retrievedAddresses, 2)
	assert.NotContains(t, retrievedAddresses, addr2)
	assert.Contains(t, retrievedAddresses, addr1)
	assert.Contains(t, retrievedAddresses, addr3)

	// Test 2: Try to remove a non-existent address from an existing trip (should fail)
	nonExistentAddr := dbt.Address("Non Existent Address")
	err = db.TripAddressListRemove(tripID, nonExistentAddr)
	assert.Error(t, err, "TripAddressListRemove should return an error for non-existent address")
	assert.Contains(t, err.Error(), "not found")
	retrievedAddresses, _ = db.GetTripAddressList(tripID)
	assert.Len(t, retrievedAddresses, 2) // Length should remain 2

	// Test 3: Try to remove address from a non-existent trip (should fail)
	nonExistentID := uuid.New()
	err = db.TripAddressListRemove(nonExistentID, addr1)
	assert.Error(t, err, "TripAddressListRemove should return an error for non-existent trip")
	assert.Contains(t, err.Error(), "not found")
}
