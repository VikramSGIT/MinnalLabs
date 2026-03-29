package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/iot-backend/internal/config"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	rdb *redis.Client
	ctx = context.Background()
	gdb *gorm.DB
)

// CapInfo holds the cached product-capability mapping.
type CapInfo struct {
	EsphomeKey       string `json:"esphome_key"`
	Component        string `json:"component"`
	TraitType        string `json:"trait_type"`
	Writable         bool   `json:"writable"`
	GoogleDeviceType string `json:"google_device_type"`
}

// DeviceInfo holds cached device metadata.
type DeviceInfo struct {
	ID        uint `json:"id"`
	UserID    uint `json:"user_id"`
	HomeID    uint `json:"home_id"`
	ProductID uint `json:"product_id"`
}

type DevicePresence struct {
	Online       bool      `json:"online"`
	LastStatus   string    `json:"last_status"`
	LastStatusAt time.Time `json:"last_status_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// --- key helpers ---

func deviceStateKey(deviceID uint) string    { return fmt.Sprintf("device:%d", deviceID) }
func deviceMetaKey(deviceID uint) string     { return fmt.Sprintf("device_meta:%d", deviceID) }
func devicePresenceKey(deviceID uint) string { return fmt.Sprintf("device_presence:%d", deviceID) }
func productCapsKey(productID uint) string   { return fmt.Sprintf("product_caps:%d", productID) }

const deviceIDsSet = "device_ids"

// --- init ---

func InitState(cfg *config.Config, db *gorm.DB) {
	gdb = db

	rdb = redis.NewClient(&redis.Options{
		Addr:     cfg.Valkey.Addr,
		Password: cfg.Valkey.Password,
		DB:       0,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Valkey: %v", err)
	}

	log.Println("Connected to Valkey")
}

// SyncProductCaps loads product-capability mappings from PostgreSQL into Valkey.
// This is the only periodic DB sync — everything else is write-through only.
func SyncProductCaps() {
	type row struct {
		ProductID        uint   `gorm:"column:product_id"`
		EsphomeKey       string `gorm:"column:esphome_key"`
		Component        string `gorm:"column:component"`
		TraitType        string `gorm:"column:trait_type"`
		Writable         bool   `gorm:"column:writable"`
		GoogleDeviceType string `gorm:"column:google_device_type"`
	}

	var rows []row
	gdb.Raw(`
		SELECT pc.product_id, pc.esphome_key, c.component, c.trait_type, c.writable, c.google_device_type
		FROM product_capabilities pc
		JOIN capabilities c ON c.id = pc.capability_id
	`).Scan(&rows)

	grouped := make(map[uint][]CapInfo)
	for _, r := range rows {
		grouped[r.ProductID] = append(grouped[r.ProductID], CapInfo{
			EsphomeKey:       r.EsphomeKey,
			Component:        r.Component,
			TraitType:        r.TraitType,
			Writable:         r.Writable,
			GoogleDeviceType: r.GoogleDeviceType,
		})
	}

	for pid, caps := range grouped {
		key := productCapsKey(pid)
		rdb.Del(ctx, key)
		fields := make(map[string]interface{})
		for _, cap := range caps {
			data, _ := json.Marshal(cap)
			fields[cap.EsphomeKey] = string(data)
		}
		if len(fields) > 0 {
			rdb.HSet(ctx, key, fields)
		}
	}

	log.Printf("Synced product caps: %d products", len(grouped))
}

// StartSync runs SyncProductCaps every 5 minutes.
func StartSync() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			SyncProductCaps()
		}
	}()
}

// ============================================================
// Product Capabilities (read from Valkey)
// ============================================================

func GetProductCaps(productID uint) ([]CapInfo, error) {
	all, err := rdb.HGetAll(ctx, productCapsKey(productID)).Result()
	if err != nil {
		return nil, err
	}
	caps := make([]CapInfo, 0, len(all))
	for _, v := range all {
		var ci CapInfo
		if err := json.Unmarshal([]byte(v), &ci); err == nil {
			caps = append(caps, ci)
		}
	}
	return caps, nil
}

func GetProductCapByKey(productID uint, esphomeKey string) (CapInfo, bool) {
	val, err := rdb.HGet(ctx, productCapsKey(productID), esphomeKey).Result()
	if err != nil {
		return CapInfo{}, false
	}
	var ci CapInfo
	if err := json.Unmarshal([]byte(val), &ci); err != nil {
		return CapInfo{}, false
	}
	return ci, true
}

// ============================================================
// Devices (write-through only, no periodic DB sync)
// ============================================================

// CacheDevice writes device metadata to Valkey. Called on enrollment.
func CacheDevice(d DeviceInfo) {
	cacheJSON(deviceMetaKey(d.ID), d)
	rdb.SAdd(ctx, deviceIDsSet, d.ID)
}

func GetDevice(id uint) (DeviceInfo, bool) {
	var di DeviceInfo
	return di, getJSON(deviceMetaKey(id), &di)
}

func GetAllDevices() []DeviceInfo {
	ids, err := rdb.SMembers(ctx, deviceIDsSet).Result()
	if err != nil {
		return nil
	}
	devices := make([]DeviceInfo, 0, len(ids))
	for _, idStr := range ids {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			continue
		}
		if di, ok := GetDevice(uint(id)); ok {
			devices = append(devices, di)
		}
	}
	return devices
}

func RemoveDevice(deviceID uint) {
	if err := rdb.Del(ctx,
		deviceMetaKey(deviceID),
		devicePresenceKey(deviceID),
		deviceStateKey(deviceID),
	).Err(); err != nil {
		log.Printf("Error removing cache entries for device %d: %v", deviceID, err)
	}

	if err := rdb.SRem(ctx, deviceIDsSet, deviceID).Err(); err != nil {
		log.Printf("Error removing device %d from tracked device set: %v", deviceID, err)
	}
}

func RemoveDevices(deviceIDs []uint) {
	for _, deviceID := range deviceIDs {
		RemoveDevice(deviceID)
	}
}

func SetDevicePresence(deviceID uint, status string) {
	normalized := strings.ToLower(strings.TrimSpace(status))
	now := time.Now().UTC()

	presence := DevicePresence{
		Online:       normalized == "online",
		LastStatus:   normalized,
		LastStatusAt: now,
	}

	if current, ok := GetDevicePresence(deviceID); ok {
		presence.LastSeenAt = current.LastSeenAt
	}
	if presence.Online {
		presence.LastSeenAt = now
	}

	cacheJSON(devicePresenceKey(deviceID), presence)
}

func GetDevicePresence(deviceID uint) (DevicePresence, bool) {
	var presence DevicePresence
	return presence, getJSON(devicePresenceKey(deviceID), &presence)
}

// ============================================================
// Device capability state (live values from MQTT)
// ============================================================

func SetCapState(deviceID uint, capKey, value string) {
	if err := rdb.HSet(ctx, deviceStateKey(deviceID), capKey, value).Err(); err != nil {
		log.Printf("Error setting state for device %d cap %s: %v", deviceID, capKey, err)
	}
}

func GetCapState(deviceID uint, capKey string) (string, error) {
	val, err := rdb.HGet(ctx, deviceStateKey(deviceID), capKey).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func GetAllCapStates(deviceID uint) (map[string]string, error) {
	return rdb.HGetAll(ctx, deviceStateKey(deviceID)).Result()
}

// ============================================================
// helpers
// ============================================================

func cacheJSON(key string, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("Error marshaling cache value for %s: %v", key, err)
		return
	}
	rdb.Set(ctx, key, string(data), 0)
}

func getJSON(key string, dest interface{}) bool {
	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return false
	}
	return json.Unmarshal([]byte(val), dest) == nil
}
