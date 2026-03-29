package ota

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/state"
)

const workerInterval = 30 * time.Second

var appCfg *config.Config

const (
	rolloutStatusPending   = "pending"
	rolloutStatusRunning   = "running"
	rolloutStatusCompleted = "completed"
	rolloutStatusCancelled = "cancelled"
	rolloutStatusFailed    = "failed"
)

const (
	rolloutDevicePending   = "pending"
	rolloutDeviceSent      = "sent"
	rolloutDeviceUpdated   = "updated"
	rolloutDeviceSkipped   = "skipped"
	rolloutDeviceFailed    = "failed"
	rolloutDeviceCancelled = "cancelled"
)

type RolloutSummary struct {
	ID                   uint       `json:"id"`
	TargetVersion        string     `json:"target_version"`
	BatchPercentage      int        `json:"batch_percentage"`
	BatchIntervalMinutes int        `json:"batch_interval_minutes"`
	Status               string     `json:"status"`
	NextBatchAt          *time.Time `json:"next_batch_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	PendingCount         int64      `json:"pending_count"`
	SentCount            int64      `json:"sent_count"`
	UpdatedCount         int64      `json:"updated_count"`
	SkippedCount         int64      `json:"skipped_count"`
	CancelledCount       int64      `json:"cancelled_count"`
	FailedCount          int64      `json:"failed_count"`
}

type DeviceRolloutInfo struct {
	RolloutID     uint   `json:"rollout_id"`
	State         string `json:"state"`
	BatchNumber   int    `json:"batch_number"`
	TargetVersion string `json:"target_version"`
}

type deviceWithSort struct {
	Device  models.Device
	SortKey string
}

func effectiveRolloutPercentage(product models.Product) int {
	if product.RolloutPercentage >= 1 && product.RolloutPercentage <= 100 {
		return product.RolloutPercentage
	}
	return 20
}

func effectiveRolloutIntervalMinutes(product models.Product) int {
	if product.RolloutIntervalMinutes > 0 {
		return product.RolloutIntervalMinutes
	}
	return 60
}

func currentDeviceFirmwareVersion(device models.Device) string {
	current := strings.TrimSpace(device.FirmwareVersion)
	if presence, found := state.GetDevicePresence(device.ID); found && strings.TrimSpace(presence.FirmwareVersion) != "" {
		current = strings.TrimSpace(presence.FirmwareVersion)
	}
	return current
}

func currentProductMD5URL(product models.Product) string {
	if strings.TrimSpace(product.FirmwareMD5URL) != "" {
		return strings.TrimSpace(product.FirmwareMD5URL)
	}
	if strings.TrimSpace(product.FirmwareURL) == "" {
		return ""
	}
	return strings.TrimSpace(product.FirmwareURL) + ".md5"
}

func currentProductFirmwareURL(product models.Product) string {
	return strings.TrimSpace(product.FirmwareURL)
}

func StartWorker(cfg *config.Config) {
	appCfg = cfg
	mqtt.RegisterStatusUpdateHook(HandleDeviceStatusUpdate)
	go func() {
		processDueRollouts()
		ticker := time.NewTicker(workerInterval)
		defer ticker.Stop()
		for {
			<-ticker.C
			processDueRollouts()
		}
	}()
}

func deterministicSortKey(rolloutID, deviceID uint) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", rolloutID, deviceID)))
	return hex.EncodeToString(sum[:])
}

func batchSize(total, percentage int) int {
	if total <= 0 {
		return 0
	}
	if percentage < 1 {
		percentage = 1
	}
	if percentage > 100 {
		percentage = 100
	}
	size := int(math.Ceil(float64(total) * float64(percentage) / 100.0))
	if size < 1 {
		return 1
	}
	return size
}

func clearRetainedForRollout(rolloutID uint) {
	type target struct {
		ID        uint `gorm:"column:id"`
		UserID    uint `gorm:"column:user_id"`
		HomeID    uint `gorm:"column:home_id"`
		ProductID uint `gorm:"column:product_id"`
	}
	var targets []target
	db.DB.Raw(`
		SELECT d.id, d.user_id, d.home_id, d.product_id
		FROM firmware_rollout_devices frd
		JOIN devices d ON d.id = frd.device_id
		WHERE frd.rollout_id = ? AND frd.state IN (?, ?)
	`, rolloutID, rolloutDevicePending, rolloutDeviceSent).Scan(&targets)

	now := time.Now().UTC()
	for _, target := range targets {
		device := models.Device{ID: target.ID, UserID: target.UserID, HomeID: target.HomeID, ProductID: target.ProductID}
		if err := mqtt.ClearRetainedDeviceFirmwareUpdate(device); err != nil {
			log.Printf("Failed clearing retained OTA command for rollout %d device %d: %v", rolloutID, target.ID, err)
			continue
		}
		db.DB.Model(&models.FirmwareRolloutDevice{}).
			Where("rollout_id = ? AND device_id = ?", rolloutID, target.ID).
			Updates(map[string]interface{}{
				"state":               rolloutDeviceCancelled,
				"retained_cleared_at": now,
				"updated_at":          now,
			})
	}
}

func cancelActiveRolloutsForProduct(productID uint) error {
	var active []models.FirmwareRollout
	if err := db.DB.
		Where("product_id = ? AND status IN ?", productID, []string{rolloutStatusPending, rolloutStatusRunning}).
		Find(&active).Error; err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, rollout := range active {
		if err := db.DB.Model(&models.FirmwareRollout{}).
			Where("id = ?", rollout.ID).
			Updates(map[string]interface{}{
				"status":        rolloutStatusCancelled,
				"next_batch_at": nil,
				"updated_at":    now,
			}).Error; err != nil {
			return err
		}
		clearRetainedForRollout(rollout.ID)
	}
	return nil
}

func CreateRollout(product models.Product, createdBy uint) (*models.FirmwareRollout, int, int, error) {
	if product.ID == 0 || product.FirmwareVersion == "" || currentProductFirmwareURL(product) == "" || currentProductMD5URL(product) == "" {
		return nil, 0, 0, fmt.Errorf("product firmware is incomplete")
	}
	batchPercentage := effectiveRolloutPercentage(product)
	batchIntervalMinutes := effectiveRolloutIntervalMinutes(product)
	if batchPercentage < 1 || batchPercentage > 100 {
		return nil, 0, 0, fmt.Errorf("batch_percentage must be between 1 and 100")
	}
	if batchIntervalMinutes < 1 {
		return nil, 0, 0, fmt.Errorf("batch_interval_minutes must be at least 1")
	}

	var devices []models.Device
	if err := db.DB.
		Select("id, user_id, home_id, product_id, firmware_version").
		Where("product_id = ? AND deleted_at IS NULL", product.ID).
		Find(&devices).Error; err != nil {
		return nil, 0, 0, fmt.Errorf("load rollout devices: %w", err)
	}

	eligible := make([]models.Device, 0, len(devices))
	for _, device := range devices {
		if currentDeviceFirmwareVersion(device) == product.FirmwareVersion {
			continue
		}
		eligible = append(eligible, device)
	}
	if len(eligible) == 0 {
		return nil, 0, 0, fmt.Errorf("no eligible devices need this firmware")
	}

	if err := cancelActiveRolloutsForProduct(product.ID); err != nil {
		return nil, 0, 0, fmt.Errorf("cancel active rollouts: %w", err)
	}

	now := time.Now().UTC()
	rollout := &models.FirmwareRollout{
		ProductID:            product.ID,
		TargetVersion:        product.FirmwareVersion,
		BatchPercentage:      batchPercentage,
		BatchIntervalMinutes: batchIntervalMinutes,
		Status:               rolloutStatusPending,
		NextBatchAt:          &now,
		CreatedByUserID:      createdBy,
	}
	if err := db.DB.Create(rollout).Error; err != nil {
		return nil, 0, 0, fmt.Errorf("create rollout: %w", err)
	}

	ordered := make([]deviceWithSort, 0, len(eligible))
	for _, device := range eligible {
		ordered = append(ordered, deviceWithSort{
			Device:  device,
			SortKey: deterministicSortKey(rollout.ID, device.ID),
		})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SortKey == ordered[j].SortKey {
			return ordered[i].Device.ID < ordered[j].Device.ID
		}
		return ordered[i].SortKey < ordered[j].SortKey
	})

	size := batchSize(len(ordered), batchPercentage)
	rows := make([]models.FirmwareRolloutDevice, 0, len(ordered))
	for idx, item := range ordered {
		rows = append(rows, models.FirmwareRolloutDevice{
			RolloutID:           rollout.ID,
			DeviceID:            item.Device.ID,
			BatchNumber:         (idx / size) + 1,
			State:               rolloutDevicePending,
			LastReportedVersion: "",
		})
	}
	if err := db.DB.Create(&rows).Error; err != nil {
		return nil, 0, 0, fmt.Errorf("create rollout devices: %w", err)
	}

	totalBatches := int(math.Ceil(float64(len(ordered)) / float64(size)))
	return rollout, len(ordered), totalBatches, nil
}

func ListRolloutsForProduct(productID uint, limit int) ([]RolloutSummary, error) {
	if limit <= 0 {
		limit = 10
	}

	var rollouts []models.FirmwareRollout
	if err := db.DB.
		Where("product_id = ?", productID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&rollouts).Error; err != nil {
		return nil, err
	}

	summaries := make([]RolloutSummary, 0, len(rollouts))
	for _, rollout := range rollouts {
		summary := RolloutSummary{
			ID:                   rollout.ID,
			TargetVersion:        rollout.TargetVersion,
			BatchPercentage:      rollout.BatchPercentage,
			BatchIntervalMinutes: rollout.BatchIntervalMinutes,
			Status:               rollout.Status,
			NextBatchAt:          rollout.NextBatchAt,
			CreatedAt:            rollout.CreatedAt,
			UpdatedAt:            rollout.UpdatedAt,
		}
		db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDevicePending).Count(&summary.PendingCount)
		db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDeviceSent).Count(&summary.SentCount)
		db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDeviceUpdated).Count(&summary.UpdatedCount)
		db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDeviceSkipped).Count(&summary.SkippedCount)
		db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDeviceCancelled).Count(&summary.CancelledCount)
		db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDeviceFailed).Count(&summary.FailedCount)
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func GetDeviceRolloutInfo(deviceID uint) (DeviceRolloutInfo, bool) {
	type row struct {
		RolloutID     uint   `gorm:"column:rollout_id"`
		State         string `gorm:"column:state"`
		BatchNumber   int    `gorm:"column:batch_number"`
		TargetVersion string `gorm:"column:target_version"`
	}

	var result row
	err := db.DB.Raw(`
		SELECT frd.rollout_id, frd.state, frd.batch_number, fr.target_version
		FROM firmware_rollout_devices frd
		JOIN firmware_rollouts fr ON fr.id = frd.rollout_id
		WHERE frd.device_id = ?
		  AND fr.status IN (?, ?)
		  AND frd.state IN (?, ?, ?)
		ORDER BY fr.created_at DESC, fr.id DESC
		LIMIT 1
	`, deviceID, rolloutStatusPending, rolloutStatusRunning, rolloutDevicePending, rolloutDeviceSent, rolloutDeviceUpdated).Scan(&result).Error
	if err != nil || result.RolloutID == 0 {
		return DeviceRolloutInfo{}, false
	}

	return DeviceRolloutInfo{
		RolloutID:     result.RolloutID,
		State:         result.State,
		BatchNumber:   result.BatchNumber,
		TargetVersion: result.TargetVersion,
	}, true
}

func MarkDeviceRolloutSent(deviceID uint) {
	var result models.FirmwareRolloutDevice
	err := db.DB.Raw(`
		SELECT frd.rollout_id, frd.device_id, frd.batch_number, frd.state, frd.sent_at, frd.updated_at, frd.retained_cleared_at, frd.last_reported_version
		FROM firmware_rollout_devices frd
		JOIN firmware_rollouts fr ON fr.id = frd.rollout_id
		WHERE frd.device_id = ?
		  AND fr.status IN (?, ?)
		  AND frd.state = ?
		ORDER BY fr.created_at DESC, fr.id DESC
		LIMIT 1
	`, deviceID, rolloutStatusPending, rolloutStatusRunning, rolloutDevicePending).Scan(&result).Error
	if err != nil || result.RolloutID == 0 {
		return
	}

	now := time.Now().UTC()
	db.DB.Model(&models.FirmwareRolloutDevice{}).
		Where("rollout_id = ? AND device_id = ?", result.RolloutID, result.DeviceID).
		Updates(map[string]interface{}{
			"state":   rolloutDeviceSent,
			"sent_at": now,
		})
}

func processDueRollouts() {
	var rollouts []models.FirmwareRollout
	now := time.Now().UTC()
	if err := db.DB.
		Where("status IN ? AND next_batch_at IS NOT NULL AND next_batch_at <= ?", []string{rolloutStatusPending, rolloutStatusRunning}, now).
		Order("next_batch_at ASC, id ASC").
		Find(&rollouts).Error; err != nil {
		log.Printf("Failed to load due rollouts: %v", err)
		return
	}

	for _, rollout := range rollouts {
		if err := dispatchNextBatch(rollout); err != nil {
			log.Printf("Failed dispatching rollout %d: %v", rollout.ID, err)
			db.DB.Model(&models.FirmwareRollout{}).Where("id = ?", rollout.ID).Updates(map[string]interface{}{
				"status":     rolloutStatusFailed,
				"updated_at": time.Now().UTC(),
			})
		}
	}
}

func dispatchNextBatch(rollout models.FirmwareRollout) error {
	type target struct {
		RolloutID       uint   `gorm:"column:rollout_id"`
		DeviceID        uint   `gorm:"column:device_id"`
		BatchNumber     int    `gorm:"column:batch_number"`
		UserID          uint   `gorm:"column:user_id"`
		HomeID          uint   `gorm:"column:home_id"`
		ProductID       uint   `gorm:"column:product_id"`
		FirmwareVersion string `gorm:"column:firmware_version"`
		TargetState     string `gorm:"column:state"`
	}

	var nextPending models.FirmwareRolloutDevice
	err := db.DB.
		Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDevicePending).
		Order("batch_number ASC, device_id ASC").
		First(&nextPending).Error
	if err != nil {
		return refreshRolloutCompletion(rollout.ID)
	}

	var targets []target
	if err := db.DB.Raw(`
		SELECT frd.rollout_id, frd.device_id, frd.batch_number, frd.state, d.user_id, d.home_id, d.product_id, d.firmware_version
		FROM firmware_rollout_devices frd
		JOIN devices d ON d.id = frd.device_id
		WHERE frd.rollout_id = ? AND frd.batch_number = ? AND frd.state = ?
		ORDER BY frd.device_id ASC
	`, rollout.ID, nextPending.BatchNumber, rolloutDevicePending).Scan(&targets).Error; err != nil {
		return err
	}
	if appCfg == nil {
		return fmt.Errorf("ota worker config not initialized")
	}

	var product models.Product
	if err := db.DB.Select("id, firmware_url, firmware_md5_url").
		First(&product, rollout.ProductID).Error; err != nil {
		return fmt.Errorf("load rollout product: %w", err)
	}
	firmwareURL := currentProductFirmwareURL(product)
	md5URL := currentProductMD5URL(product)
	if firmwareURL == "" || md5URL == "" {
		return fmt.Errorf("product firmware is incomplete")
	}

	now := time.Now().UTC()
	for _, target := range targets {
		currentVersion := strings.TrimSpace(target.FirmwareVersion)
		if presence, found := state.GetDevicePresence(target.DeviceID); found && strings.TrimSpace(presence.FirmwareVersion) != "" {
			currentVersion = strings.TrimSpace(presence.FirmwareVersion)
		}
		if currentVersion != "" && currentVersion == rollout.TargetVersion {
			if err := db.DB.Model(&models.FirmwareRolloutDevice{}).
				Where("rollout_id = ? AND device_id = ?", rollout.ID, target.DeviceID).
				Updates(map[string]interface{}{
					"state":                 rolloutDeviceUpdated,
					"updated_at":            now,
					"last_reported_version": currentVersion,
				}).Error; err != nil {
				return err
			}
			continue
		}

		device := models.Device{
			ID:        target.DeviceID,
			UserID:    target.UserID,
			HomeID:    target.HomeID,
			ProductID: target.ProductID,
		}
		if err := mqtt.PublishRetainedDeviceFirmwareUpdate(
			device,
			rollout.TargetVersion,
			firmwareURL,
			md5URL,
			rollout.ID,
			target.BatchNumber,
		); err != nil {
			return err
		}
		if err := db.DB.Model(&models.FirmwareRolloutDevice{}).
			Where("rollout_id = ? AND device_id = ?", rollout.ID, target.DeviceID).
			Updates(map[string]interface{}{
				"state":   rolloutDeviceSent,
				"sent_at": now,
			}).Error; err != nil {
			return err
		}
	}

	var pendingCount int64
	var sentCount int64
	db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDevicePending).Count(&pendingCount)
	db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rollout.ID, rolloutDeviceSent).Count(&sentCount)

	updates := map[string]interface{}{
		"status":     rolloutStatusRunning,
		"updated_at": now,
	}
	if pendingCount > 0 {
		nextBatchAt := now.Add(time.Duration(rollout.BatchIntervalMinutes) * time.Minute)
		updates["next_batch_at"] = nextBatchAt
	} else {
		updates["next_batch_at"] = nil
		if sentCount == 0 {
			updates["status"] = rolloutStatusCompleted
		}
	}

	return db.DB.Model(&models.FirmwareRollout{}).Where("id = ?", rollout.ID).Updates(updates).Error
}

func refreshRolloutCompletion(rolloutID uint) error {
	var pendingCount int64
	var sentCount int64
	db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rolloutID, rolloutDevicePending).Count(&pendingCount)
	db.DB.Model(&models.FirmwareRolloutDevice{}).Where("rollout_id = ? AND state = ?", rolloutID, rolloutDeviceSent).Count(&sentCount)

	now := time.Now().UTC()
	status := rolloutStatusRunning
	var nextBatchAt interface{} = nil
	if pendingCount == 0 && sentCount == 0 {
		status = rolloutStatusCompleted
	}

	return db.DB.Model(&models.FirmwareRollout{}).
		Where("id = ?", rolloutID).
		Updates(map[string]interface{}{
			"status":        status,
			"next_batch_at": nextBatchAt,
			"updated_at":    now,
		}).Error
}

func HandleDeviceStatusUpdate(deviceID uint, status string) {
	raw := strings.TrimSpace(status)
	if !strings.HasPrefix(strings.ToLower(raw), "online_") || len(raw) <= len("online_") {
		return
	}
	version := strings.TrimSpace(raw[len("online_"):])
	now := time.Now().UTC()

	var device models.Device
	if err := db.DB.Preload("Product").First(&device, deviceID).Error; err == nil {
		if device.Product.FirmwareVersion != "" && version == device.Product.FirmwareVersion {
			if err := mqtt.ClearRetainedDeviceFirmwareUpdate(device); err != nil {
				log.Printf("Failed clearing retained OTA command for device %d: %v", deviceID, err)
			}
		}
	}

	type match struct {
		RolloutID     uint   `gorm:"column:rollout_id"`
		DeviceID      uint   `gorm:"column:device_id"`
		UserID        uint   `gorm:"column:user_id"`
		HomeID        uint   `gorm:"column:home_id"`
		ProductID     uint   `gorm:"column:product_id"`
		TargetVersion string `gorm:"column:target_version"`
	}
	var matches []match
	db.DB.Raw(`
		SELECT frd.rollout_id, frd.device_id, d.user_id, d.home_id, d.product_id, fr.target_version
		FROM firmware_rollout_devices frd
		JOIN firmware_rollouts fr ON fr.id = frd.rollout_id
		JOIN devices d ON d.id = frd.device_id
		WHERE frd.device_id = ?
		  AND fr.status IN (?, ?)
		  AND frd.state IN (?, ?)
	`, deviceID, rolloutStatusPending, rolloutStatusRunning, rolloutDevicePending, rolloutDeviceSent).Scan(&matches)

	for _, match := range matches {
		db.DB.Model(&models.FirmwareRolloutDevice{}).
			Where("rollout_id = ? AND device_id = ?", match.RolloutID, match.DeviceID).
			Update("last_reported_version", version)

		if version != match.TargetVersion {
			continue
		}

		retainedDevice := models.Device{
			ID:        match.DeviceID,
			UserID:    match.UserID,
			HomeID:    match.HomeID,
			ProductID: match.ProductID,
		}
		retainedClearedAt := (*time.Time)(nil)
		if err := mqtt.ClearRetainedDeviceFirmwareUpdate(retainedDevice); err == nil {
			cleared := now
			retainedClearedAt = &cleared
		} else {
			log.Printf("Failed clearing retained OTA command for device %d: %v", match.DeviceID, err)
		}

		updates := map[string]interface{}{
			"state":                 rolloutDeviceUpdated,
			"updated_at":            now,
			"last_reported_version": version,
		}
		if retainedClearedAt != nil {
			updates["retained_cleared_at"] = *retainedClearedAt
		}
		db.DB.Model(&models.FirmwareRolloutDevice{}).
			Where("rollout_id = ? AND device_id = ?", match.RolloutID, match.DeviceID).
			Updates(updates)

		if err := refreshRolloutCompletion(match.RolloutID); err != nil {
			log.Printf("Failed refreshing rollout %d: %v", match.RolloutID, err)
		}
	}
}
