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
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID        uuid.UUID `gorm:"type:uuid;not null"`
	Name          string    `gorm:"size:255;not null"`
	Amount        float64   `gorm:"type:numeric(10,2);not null"`
	Time          time.Time `gorm:"not null"` // Use time.Time to store the timestamp
	PrePayAddress string    `gorm:"size:255;not null"`
	Category      int       `gorm:"not null"` // Use int to store the category
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
	RecordID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	Address     string    `gorm:"size:255;primaryKey"`
	ExtendedMsg float64   `gorm:"type:numeric(10,2)"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (RecordShouldPayAddressListModel) TableName() string {
	return "record_should_pay_address_lists"
}

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
