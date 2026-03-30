package models

import (
	"fmt"
	"strings"
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
	ID                 uint           `gorm:"primarykey" json:"id"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
	UserID             uint           `json:"user_id"`
	Name               string         `json:"name"`
	WiFiSSID           string         `gorm:"column:wifi_ssid" json:"wifi_ssid"`
	WiFiPassword       string         `gorm:"column:wifi_password" json:"-"`
	MQTTUsername       string         `gorm:"column:mqtt_username" json:"mqtt_username"`
	MQTTPassword       string         `gorm:"column:mqtt_password" json:"-"`
	MQTTProvisionState string         `gorm:"column:mqtt_provision_state" json:"mqtt_provision_state"`
	MQTTProvisionError string         `gorm:"column:mqtt_provision_error" json:"mqtt_provision_error,omitempty"`
	MQTTProvisionedAt  *time.Time     `gorm:"column:mqtt_provisioned_at" json:"mqtt_provisioned_at,omitempty"`
	Devices            []Device       `json:"devices,omitempty"`
}

const (
	HomeMQTTProvisionStatePending  = "pending"
	HomeMQTTProvisionStateReady    = "ready"
	HomeMQTTProvisionStateFailed   = "failed"
	HomeMQTTProvisionStateDeleting = "deleting"

	HomeMQTTJobOperationProvision = "provision"
	HomeMQTTJobOperationCleanup   = "cleanup"

	HomeMQTTJobStatusPending = "pending"
	HomeMQTTJobStatusRunning = "running"
	HomeMQTTJobStatusFailed  = "failed"
)

func (h Home) MQTTState() string {
	state := strings.TrimSpace(h.MQTTProvisionState)
	switch state {
	case HomeMQTTProvisionStatePending, HomeMQTTProvisionStateReady, HomeMQTTProvisionStateFailed, HomeMQTTProvisionStateDeleting:
		return state
	default:
		if h.DeletedAt.Valid {
			return HomeMQTTProvisionStateDeleting
		}
		if strings.TrimSpace(h.MQTTUsername) == "" || strings.TrimSpace(h.MQTTPassword) == "" {
			return HomeMQTTProvisionStateFailed
		}
		return HomeMQTTProvisionStateReady
	}
}

func (h Home) AllowsDeviceProvisioning() bool {
	switch h.MQTTState() {
	case HomeMQTTProvisionStatePending, HomeMQTTProvisionStateReady:
		return true
	default:
		return false
	}
}

func (h Home) HasMQTTCredentials() bool {
	return strings.TrimSpace(h.MQTTUsername) != "" && strings.TrimSpace(h.MQTTPassword) != ""
}

type Product struct {
	ID                     uint                `gorm:"primarykey" json:"id"`
	Name                   string              `gorm:"uniqueIndex" json:"name"`
	FirmwareVersion        string              `gorm:"column:firmware_version" json:"firmware_version"`
	FirmwareURL            string              `gorm:"column:firmware_url" json:"firmware_url"`
	FirmwareMD5URL         string              `gorm:"column:firmware_md5_url" json:"firmware_md5_url"`
	FirmwareUploadedAt     *time.Time          `gorm:"column:firmware_uploaded_at" json:"firmware_uploaded_at,omitempty"`
	RolloutPercentage      int                 `gorm:"column:rollout_percentage" json:"rollout_percentage"`
	RolloutIntervalMinutes int                 `gorm:"column:rollout_interval_minutes" json:"rollout_interval_minutes"`
	ProductCapabilities    []ProductCapability `json:"product_capabilities,omitempty"`
}

const (
	DefaultRolloutPercentage      = 20
	DefaultRolloutIntervalMinutes = 60
)

func (p Product) EffectiveRolloutPercentage() int {
	if p.RolloutPercentage >= 1 && p.RolloutPercentage <= 100 {
		return p.RolloutPercentage
	}
	return DefaultRolloutPercentage
}

func (p Product) EffectiveRolloutIntervalMinutes() int {
	if p.RolloutIntervalMinutes > 0 {
		return p.RolloutIntervalMinutes
	}
	return DefaultRolloutIntervalMinutes
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
	ID              uint           `gorm:"primarykey" json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
	UserID          uint           `json:"user_id"`
	HomeID          uint           `json:"home_id"`
	ProductID       uint           `json:"product_id"`
	Name            string         `json:"name"`
	FirmwareVersion string         `gorm:"column:firmware_version" json:"firmware_version"`
	DevicePublicKey string         `gorm:"column:device_public_key" json:"-"`
	Product         Product        `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	Home            Home           `gorm:"foreignKey:HomeID" json:"home,omitempty"`
}

type FirmwareRollout struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	ProductID            uint       `json:"product_id"`
	TargetVersion        string     `json:"target_version"`
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

type HomeMQTTJob struct {
	ID        uint       `gorm:"primaryKey"`
	HomeID    uint       `gorm:"column:home_id"`
	Operation string     `gorm:"column:operation"`
	Status    string     `gorm:"column:status"`
	Attempts  int        `gorm:"column:attempts"`
	NextRunAt time.Time  `gorm:"column:next_run_at"`
	ClaimedAt *time.Time `gorm:"column:claimed_at"`
	LastError string     `gorm:"column:last_error"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
}

func (HomeMQTTJob) TableName() string { return "home_mqtt_jobs" }

// OAuth models

type OAuthClient struct {
	ID     string `gorm:"primaryKey"`
	Secret string
	Domain string
	UserID string
}

func (OAuthClient) TableName() string { return "oauth_clients" }

type OAuthToken struct {
	ID        uint       `gorm:"primaryKey"`
	ClientID  string     `gorm:"column:client_id"`
	UserID    string     `gorm:"column:user_id"`
	Code      string     `gorm:"column:code"`
	Access    string     `gorm:"column:access"`
	Refresh   string     `gorm:"column:refresh"`
	Data      string     `gorm:"column:data"`
	ExpiresIn int64      `gorm:"column:expires_in"`
	ExpiresAt *time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
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
	return fmt.Sprintf("%d/%d/%d/firmware_update", userID, homeID, deviceID)
}

func BuildFirmwareBaseName(productID uint, version string) string {
	return fmt.Sprintf("%d_%s", productID, strings.TrimSpace(version))
}

func BuildFirmwareFilename(productID uint, version string) string {
	return BuildFirmwareBaseName(productID, version) + ".bin"
}

func BuildFirmwareMD5Filename(productID uint, version string) string {
	return BuildFirmwareBaseName(productID, version) + ".bin.md5"
}

func firmwareBaseDir(rawURL string) string {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if rawURL == "" {
		return ""
	}

	lower := strings.ToLower(rawURL)
	if strings.HasSuffix(lower, ".bin.md5") || strings.HasSuffix(lower, ".bin") || strings.HasSuffix(lower, ".md5") {
		idx := strings.LastIndex(rawURL, "/")
		if idx <= 0 {
			return ""
		}
		rawURL = rawURL[:idx]
	}
	return strings.TrimRight(rawURL, "/")
}

func DeriveFirmwareURL(baseURL string, productID uint, version string) string {
	baseURL = firmwareBaseDir(baseURL)
	version = strings.TrimSpace(version)
	if baseURL == "" || productID == 0 || version == "" {
		return ""
	}

	if strings.HasSuffix(strings.ToLower(baseURL), "/firmware") {
		return baseURL + "/" + BuildFirmwareFilename(productID, version)
	}
	return baseURL + "/firmware/" + BuildFirmwareFilename(productID, version)
}

func DeriveFirmwareMD5URL(firmwareURLBase, md5URLBase string, productID uint, version string) string {
	md5URLBase = firmwareBaseDir(md5URLBase)
	version = strings.TrimSpace(version)
	if productID == 0 || version == "" {
		return ""
	}

	if md5URLBase != "" {
		if strings.HasSuffix(strings.ToLower(md5URLBase), "/firmware") {
			return md5URLBase + "/" + BuildFirmwareMD5Filename(productID, version)
		}
		return md5URLBase + "/firmware/" + BuildFirmwareMD5Filename(productID, version)
	}

	firmwareURL := DeriveFirmwareURL(firmwareURLBase, productID, version)
	if firmwareURL == "" {
		return ""
	}
	return firmwareURL + ".md5"
}
