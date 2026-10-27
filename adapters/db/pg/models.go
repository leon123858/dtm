package pg

import (
	"time"

	"github.com/google/uuid"
)

type TripInfoModel struct {
	Private bool      `gorm:"not null;default:false"`
	ID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name    string    `gorm:"size:255;not null"`
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
	UseNickname bool      `gorm:"not null;default:false"`
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID      uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_addresses_trip_name,priority:1"`
	Name        string    `gorm:"size:255;not null;uniqueIndex:idx_addresses_trip_name,priority:2"`
	// meta data
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for AddressModel.
func (AddressModel) TableName() string {
	return "addresses"
}

// AuthEntryModel maps the short-lived, single-use authentication entries.
type AuthEntryModel struct {
	Kind        string `gorm:"primaryKey"`
	KeyHash     string `gorm:"primaryKey"`
	BindingHash string
	Payload     []byte
	ExpiresAt   time.Time
}

func (AuthEntryModel) TableName() string { return "auth_entries" }

// UserModel maps a GCIP identity to a local member.
type UserModel struct {
	Name    *string
	Email   *string
	SubMail bool      `gorm:"not null;default:false"`
	ID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	GCID    string    `gorm:"column:gcid;not null;uniqueIndex"`
}

func (UserModel) TableName() string { return "users" }

type TripSubscriptionModel struct {
	LastViewedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UserID       uuid.UUID `gorm:"type:uuid;primaryKey"`
	TripID       uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (TripSubscriptionModel) TableName() string { return "trip_subscriptions" }

type TripBindingModel struct {
	UserID    uuid.UUID  `gorm:"type:uuid;primaryKey"`
	TripID    uuid.UUID  `gorm:"type:uuid;primaryKey"`
	AddressID *uuid.UUID `gorm:"type:uuid"`
}

func (TripBindingModel) TableName() string { return "trip_bindings" }
