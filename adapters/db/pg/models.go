package pg

import (
	"time"

	"github.com/google/uuid"
)

type TripInfoModel struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name string    `gorm:"size:255;not null"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for TripInfoModel.
func (TripInfoModel) TableName() string {
	return "trips"
}

type RecordModel struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`
	TripID          uuid.UUID  `gorm:"type:uuid;not null"`
	ParentRecordID  *uuid.UUID `gorm:"type:uuid"`
	ChildRecordID   *uuid.UUID `gorm:"type:uuid"`
	Name            string     `gorm:"size:255;not null"`
	Amount          float64    `gorm:"type:numeric(10,2);not null"`
	Time            time.Time  `gorm:"not null"` // Use time.Time to store the timestamp
	PrePayAddressID uuid.UUID  `gorm:"type:uuid;not null"`
	Category        int        `gorm:"not null"` // Use int to store the category
	IsDeleted       bool       `gorm:"not null;default:false"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for RecordModel.
func (RecordModel) TableName() string {
	return "records"
}

type RecordShouldPayAddressListModel struct {
	RecordID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID      uuid.UUID `gorm:"type:uuid;not null"`
	AddressID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	ExtendedMsg float64   `gorm:"type:numeric(10,2)"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (RecordShouldPayAddressListModel) TableName() string {
	return "record_should_pay_address_lists"
}

type AddressModel struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_addresses_trip_name,priority:1"`
	Name   string    `gorm:"size:255;not null;uniqueIndex:idx_addresses_trip_name,priority:2"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for AddressModel.
func (AddressModel) TableName() string {
	return "addresses"
}
