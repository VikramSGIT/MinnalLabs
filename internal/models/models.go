package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Username  string         `gorm:"uniqueIndex" json:"username"`
	Password  string         `json:"-"`
	Homes     []Home         `json:"homes,omitempty"`
}

type AdminUser struct {
	UserID    uint      `gorm:"primaryKey" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (AdminUser) TableName() string { return "admin_users" }

type Home struct {
	ID           uint           `gorm:"primarykey" json:"id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	UserID       uint           `json:"user_id"`
	Name         string         `json:"name"`
	WiFiSSID     string         `gorm:"column:wifi_ssid" json:"wifi_ssid"`
	WiFiPassword string         `gorm:"column:wifi_password" json:"-"`
	MQTTUsername string         `gorm:"column:mqtt_username" json:"mqtt_username"`
	MQTTPassword string         `gorm:"column:mqtt_password" json:"-"`
	Devices      []Device       `json:"devices,omitempty"`
}

type Product struct {
	ID                  uint                `gorm:"primarykey" json:"id"`
	Name                string              `gorm:"uniqueIndex" json:"name"`
	FirmwareVersion     string              `gorm:"column:firmware_version" json:"firmware_version"`
	FirmwareFilename    string              `gorm:"column:firmware_filename" json:"firmware_filename"`
	FirmwareMD5         string              `gorm:"column:firmware_md5" json:"firmware_md5"`
	FirmwareUploadedAt  *time.Time          `gorm:"column:firmware_uploaded_at" json:"firmware_uploaded_at,omitempty"`
	RolloutDelayDays    int                 `gorm:"column:rollout_delay_days" json:"rollout_delay_days"`
	ProductCapabilities []ProductCapability `json:"product_capabilities,omitempty"`
}

type Capability struct {
	ID               uint   `gorm:"primarykey" json:"id"`
	Component        string `json:"component"`
	TraitType        string `json:"trait_type"`
	Writable         bool   `json:"writable"`
	GoogleDeviceType string `json:"google_device_type"`
}

type ProductCapability struct {
	ProductID    uint       `gorm:"primaryKey" json:"product_id"`
	CapabilityID uint       `gorm:"primaryKey" json:"capability_id"`
	EsphomeKey   string     `gorm:"primaryKey" json:"esphome_key"`
	Capability   Capability `gorm:"foreignKey:CapabilityID" json:"capability,omitempty"`
}

type Device struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	UserID    uint           `json:"user_id"`
	HomeID    uint           `json:"home_id"`
	ProductID uint           `json:"product_id"`
	Name      string         `json:"name"`
	Product   Product        `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	Home      Home           `gorm:"foreignKey:HomeID" json:"home,omitempty"`
}

type FirmwareRollout struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	ProductID            uint       `json:"product_id"`
	TargetVersion        string     `json:"target_version"`
	FirmwareFilename     string     `json:"firmware_filename"`
	FirmwareMD5          string     `json:"firmware_md5"`
	BatchPercentage      int        `json:"batch_percentage"`
	BatchIntervalMinutes int        `json:"batch_interval_minutes"`
	Status               string     `json:"status"`
	NextBatchAt          *time.Time `json:"next_batch_at,omitempty"`
	CreatedByUserID      uint       `json:"created_by_user_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (FirmwareRollout) TableName() string { return "firmware_rollouts" }

type FirmwareRolloutDevice struct {
	RolloutID           uint       `gorm:"primaryKey" json:"rollout_id"`
	DeviceID            uint       `gorm:"primaryKey" json:"device_id"`
	BatchNumber         int        `json:"batch_number"`
	State               string     `json:"state"`
	SentAt              *time.Time `json:"sent_at,omitempty"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
	RetainedClearedAt   *time.Time `json:"retained_cleared_at,omitempty"`
	LastReportedVersion string     `json:"last_reported_version"`
}

func (FirmwareRolloutDevice) TableName() string { return "firmware_rollout_devices" }

// OAuth models

type OAuthClient struct {
	ID     string `gorm:"primaryKey"`
	Secret string
	Domain string
	UserID string
}

func (OAuthClient) TableName() string { return "oauth_clients" }

type OAuthToken struct {
	ID        uint `gorm:"primaryKey"`
	ClientID  string
	UserID    string
	Access    string
	Refresh   string
	ExpiresIn time.Duration
	CreatedAt time.Time
}

func (OAuthToken) TableName() string { return "oauth_tokens" }

// BuildTopic constructs an MQTT topic from device/capability data.
// action is either "state" or "command".
func BuildTopic(userID, homeID, deviceID uint, component, esphomeKey, action string) string {
	return fmt.Sprintf("%d/%d/%d/%s/%s/%s", userID, homeID, deviceID, component, esphomeKey, action)
}

func BuildStatusTopic(userID, homeID, deviceID uint) string {
	return fmt.Sprintf("%d/%d/%d/status", userID, homeID, deviceID)
}

func BuildOTACommandTopic(userID, homeID, deviceID uint) string {
	return fmt.Sprintf("%d/%d/%d/ota/command", userID, homeID, deviceID)
}
