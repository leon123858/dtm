package pg

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// TripInfoModel 代表 trips 表
type TripInfoModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name      string    `gorm:"size:255;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	// Records 和 AddressList 將透過關聯管理，而不是直接儲存在此模型中
}

// TableName returns the table name for TripInfoModel.
func (TripInfoModel) TableName() string {
	return "trips"
}

// RecordModel 代表 records 表
type RecordModel struct {
	ID               uuid.UUID      `gorm:"type:uuid;primaryKey"`
	TripID           uuid.UUID      `gorm:"type:uuid;not null"`                                                            // 外鍵
	TripInfo         TripInfoModel  `gorm:"foreignKey:TripID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"` // 新增的 GORM 關係
	Name             string         `gorm:"size:255;not null"`
	Amount           float64        `gorm:"type:numeric(10,2);not null"`
	PrePayAddress    string         `gorm:"size:255;not null"`
	ShouldPayAddress pq.StringArray `gorm:"type:text[];not null;default:'{}'"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TableName returns the table name for RecordModel.
func (RecordModel) TableName() string {
	return "records"
}

// TripAddressListModel 代表 trip_address_lists 表
type TripAddressListModel struct {
	TripID    uuid.UUID     `gorm:"type:uuid;primaryKey"`                                                          // 複合主鍵的一部分
	TripInfo  TripInfoModel `gorm:"foreignKey:TripID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"` // 新增的 GORM 關係
	Address   string        `gorm:"size:255;primaryKey"`                                                           // 複合主鍵的一部分
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName returns the table name for TripAddressListModel.
func (TripAddressListModel) TableName() string {
	return "trip_address_lists"
}
