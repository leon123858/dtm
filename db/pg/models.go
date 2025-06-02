package pg

import (
	"time"

	"github.com/google/uuid"
)

// TripInfoModel 代表 trips 表
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

// RecordModel 代表 records 表
type RecordModel struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID        uuid.UUID `gorm:"type:uuid;not null"`
	Name          string    `gorm:"size:255;not null"`
	Amount        float64   `gorm:"type:numeric(10,2);not null"`
	PrePayAddress string    `gorm:"size:255;not null"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for RecordModel.
func (RecordModel) TableName() string {
	return "records"
}

// RecordShouldPayAddressListModel
type RecordShouldPayAddressListModel struct {
	RecordID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	Address  string    `gorm:"size:255;primaryKey"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (RecordShouldPayAddressListModel) TableName() string {
	return "record_should_pay_address_lists"
}

// TripAddressListModel 代表 trip_address_lists 表
type TripAddressListModel struct {
	TripID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	Address string    `gorm:"size:255;primaryKey"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for TripAddressListModel.
func (TripAddressListModel) TableName() string {
	return "trip_address_lists"
}
