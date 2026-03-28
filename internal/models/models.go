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

type Home struct {
	ID           uint           `gorm:"primarykey" json:"id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	UserID       uint           `json:"user_id"`
	Name         string         `json:"name"`
	WiFiSSID     string         `json:"wifi_ssid"`
	WiFiPassword string         `json:"-"`
	MQTTUsername string         `json:"mqtt_username"`
	MQTTPassword string         `json:"-"`
	Devices      []Device       `json:"devices,omitempty"`
}

type Product struct {
	ID                  uint                `gorm:"primarykey" json:"id"`
	Name                string              `gorm:"uniqueIndex" json:"name"`
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

// OAuth models

type OAuthClient struct {
	ID     string `gorm:"primaryKey"`
	Secret string
	Domain string
	UserID string
}

type OAuthToken struct {
	ID        uint `gorm:"primaryKey"`
	ClientID  string
	UserID    string
	Access    string
	Refresh   string
	ExpiresIn time.Duration
	CreatedAt time.Time
}

// BuildTopic constructs an MQTT topic from device/capability data.
// action is either "state" or "command".
func BuildTopic(userID, homeID, deviceID uint, component, esphomeKey, action string) string {
	return fmt.Sprintf("%d/%d/%d/%s/%s/%s", userID, homeID, deviceID, component, esphomeKey, action)
}
