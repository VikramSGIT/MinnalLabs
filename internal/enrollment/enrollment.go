package enrollment

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	devcrypto "github.com/iot-backend/internal/crypto"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/oauth"
	"github.com/iot-backend/internal/ota"
	"github.com/iot-backend/internal/state"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var cfg *config.Config

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func generateHomeMQTTUsername(userID, homeID uint) (string, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("home_%d_%d_%s", userID, homeID, suffix), nil
}

func generateHomeMQTTPassword() (string, error) {
	return randomHex(18)
}

func SetupEnrollmentRoutes(r *gin.Engine, appCfg *config.Config) {
	cfg = appCfg

	api := r.Group("/api/enroll")
	{
		api.POST("/user", enrollUser)

		protected := api.Group("")
		protected.Use(oauth.RequireSession())
		protected.GET("/homes", listHomes)
		protected.GET("/home/:homeID/devices", listHomeDevices)
		protected.DELETE("/home/:homeID", deleteHome)
		protected.DELETE("/device/:deviceID", deleteDevice)
		protected.GET("/device/:deviceID/status", getDeviceStatus)
		protected.POST("/device/:deviceID/update", triggerDeviceUpdate)
		protected.POST("/home", enrollHome)
		protected.POST("/device", enrollDevice)
	}
}

func enrollUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	user := models.User{
		Username: req.Username,
		Password: string(hashed),
	}
	if err := db.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user_id":  user.ID,
		"username": user.Username,
	})
}

func listHomes(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var homes []models.Home
	if err := db.DB.
		Where("user_id = ?", sessionUser.UserID).
		Order("name ASC, id ASC").
		Find(&homes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load homes"})
		return
	}

	response := make([]gin.H, 0, len(homes))
	for _, home := range homes {
		response = append(response, gin.H{
			"home_id": home.ID,
			"name":    home.Name,
		})
	}

	c.JSON(http.StatusOK, response)
}

func firmwareState(device models.Device, presence state.DevicePresence, presenceFound bool) (string, string, bool) {
	current := strings.TrimSpace(device.FirmwareVersion)
	if presenceFound && strings.TrimSpace(presence.FirmwareVersion) != "" {
		current = strings.TrimSpace(presence.FirmwareVersion)
	}
	target := strings.TrimSpace(device.Product.FirmwareVersion)
	updateAvailable := current != "" && target != "" && current != target
	return current, target, updateAvailable
}

func firmwareMD5URL(product models.Product) string {
	return models.DeriveFirmwareMD5URL(product.FirmwareURL, product.FirmwareMD5URL, product.ID, product.FirmwareVersion)
}

func firmwareURL(product models.Product) string {
	return models.DeriveFirmwareURL(product.FirmwareURL, product.ID, product.FirmwareVersion)
}

func listHomeDevices(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	homeIDValue, err := strconv.ParseUint(c.Param("homeID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid home id"})
		return
	}
	homeID := uint(homeIDValue)

	var home models.Home
	if err := db.DB.Select("id, user_id").First(&home, homeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "home not found"})
		return
	}
	if home.UserID != sessionUser.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "home does not belong to user"})
		return
	}

	var devices []models.Device
	if err := db.DB.
		Preload("Product").
		Where("home_id = ?", homeID).
		Order("created_at DESC, id DESC").
		Find(&devices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load devices"})
		return
	}

	response := make([]gin.H, 0, len(devices))
	for _, device := range devices {
		currentFirmware, targetFirmware, updateAvailable := firmwareState(device, state.DevicePresence{}, false)
		item := gin.H{
			"device_id":               device.ID,
			"name":                    device.Name,
			"product_id":              device.ProductID,
			"mqtt_connected":          false,
			"mqtt_status":             "unknown",
			"firmware_version":        currentFirmware,
			"target_firmware_version": targetFirmware,
			"update_available":        updateAvailable,
			"rollout_state":           "",
			"rollout_batch_number":    0,
			"rollout_id":              0,
			"created_at":              device.CreatedAt,
		}

		if presence, found := state.GetDevicePresence(device.ID); found {
			item["mqtt_connected"] = presence.Online
			item["mqtt_status"] = presence.LastStatus
			currentFirmware, targetFirmware, updateAvailable = firmwareState(device, presence, true)
			item["firmware_version"] = currentFirmware
			item["target_firmware_version"] = targetFirmware
			item["update_available"] = updateAvailable
			if !presence.LastStatusAt.IsZero() {
				item["last_status_at"] = presence.LastStatusAt
			}
			if !presence.LastSeenAt.IsZero() {
				item["last_seen_at"] = presence.LastSeenAt
			}
		}
		if rolloutInfo, found := ota.GetDeviceRolloutInfo(device.ID); found {
			item["rollout_state"] = rolloutInfo.State
			item["rollout_batch_number"] = rolloutInfo.BatchNumber
			item["rollout_id"] = rolloutInfo.RolloutID
			if item["target_firmware_version"] == "" {
				item["target_firmware_version"] = rolloutInfo.TargetVersion
			}
		}

		response = append(response, item)
	}

	c.JSON(http.StatusOK, response)
}

func getDeviceStatus(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	deviceIDValue, err := strconv.ParseUint(c.Param("deviceID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	deviceID := uint(deviceIDValue)

	var device models.Device
	if err := db.DB.Preload("Product").Select("id, user_id, product_id, firmware_version").First(&device, deviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	if device.UserID != sessionUser.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "device does not belong to user"})
		return
	}

	presence, found := state.GetDevicePresence(deviceID)
	if !found {
		currentFirmware, targetFirmware, updateAvailable := firmwareState(device, state.DevicePresence{}, false)
		c.JSON(http.StatusOK, gin.H{
			"device_id":               deviceID,
			"mqtt_connected":          false,
			"mqtt_status":             "unknown",
			"firmware_version":        currentFirmware,
			"target_firmware_version": targetFirmware,
			"update_available":        updateAvailable,
			"rollout_state":           "",
			"rollout_batch_number":    0,
			"rollout_id":              0,
		})
		return
	}

	currentFirmware, targetFirmware, updateAvailable := firmwareState(device, presence, true)
	response := gin.H{
		"device_id":               deviceID,
		"mqtt_connected":          presence.Online,
		"mqtt_status":             presence.LastStatus,
		"firmware_version":        currentFirmware,
		"target_firmware_version": targetFirmware,
		"update_available":        updateAvailable,
		"rollout_state":           "",
		"rollout_batch_number":    0,
		"rollout_id":              0,
		"last_status_at":          presence.LastStatusAt,
		"last_seen_at":            presence.LastSeenAt,
	}
	if rolloutInfo, found := ota.GetDeviceRolloutInfo(device.ID); found {
		response["rollout_state"] = rolloutInfo.State
		response["rollout_batch_number"] = rolloutInfo.BatchNumber
		response["rollout_id"] = rolloutInfo.RolloutID
		if response["target_firmware_version"] == "" {
			response["target_firmware_version"] = rolloutInfo.TargetVersion
		}
	}
	c.JSON(http.StatusOK, response)
}

func triggerDeviceUpdate(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	deviceIDValue, err := strconv.ParseUint(c.Param("deviceID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	deviceID := uint(deviceIDValue)

	var device models.Device
	if err := db.DB.Preload("Product").First(&device, deviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	if device.UserID != sessionUser.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "device does not belong to user"})
		return
	}
	md5URL := firmwareMD5URL(device.Product)
	otaURL := firmwareURL(device.Product)
	if device.Product.FirmwareVersion == "" || otaURL == "" || md5URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no firmware uploaded for this product"})
		return
	}

	currentFirmware, _, _ := firmwareState(device, state.DevicePresence{}, false)
	if presence, found := state.GetDevicePresence(device.ID); found {
		currentFirmware, _, _ = firmwareState(device, presence, true)
	}
	if currentFirmware != "" && currentFirmware == device.Product.FirmwareVersion {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device already has the latest firmware"})
		return
	}

	if err := mqtt.PublishDeviceFirmwareUpdateNow(
		device,
		device.Product.FirmwareVersion,
		otaURL,
		md5URL,
	); err != nil {
		log.Printf("Failed to publish firmware update for device %d: %v", device.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to queue firmware update"})
		return
	}
	ota.MarkDeviceRolloutSent(device.ID)

	c.JSON(http.StatusOK, gin.H{
		"device_id":        device.ID,
		"product_id":       device.ProductID,
		"firmware_version": device.Product.FirmwareVersion,
		"firmware_url":     otaURL,
		"firmware_md5_url": md5URL,
		"queued":           true,
	})
}

func deleteHome(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	homeIDValue, err := strconv.ParseUint(c.Param("homeID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid home id"})
		return
	}
	homeID := uint(homeIDValue)

	var home models.Home
	if err := db.DB.First(&home, homeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "home not found"})
		return
	}
	if home.UserID != sessionUser.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "home does not belong to user"})
		return
	}

	var devices []models.Device
	if err := db.DB.Where("home_id = ?", homeID).Find(&devices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load home devices"})
		return
	}

	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if len(devices) > 0 {
			deviceIDs := make([]uint, 0, len(devices))
			for _, device := range devices {
				deviceIDs = append(deviceIDs, device.ID)
			}

			if err := tx.Unscoped().Where("id IN ?", deviceIDs).Delete(&models.Device{}).Error; err != nil {
				return fmt.Errorf("delete home devices: %w", err)
			}
		}

		if err := tx.Unscoped().Delete(&home).Error; err != nil {
			return fmt.Errorf("delete home: %w", err)
		}

		return nil
	}); err != nil {
		log.Printf("Failed to delete home %d: %v", homeID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete home"})
		return
	}

	if home.MQTTUsername != "" {
		if err := mqtt.CleanupHomeAccess(home.UserID, home.ID, home.MQTTUsername); err != nil {
			log.Printf("Failed to clean up MQTT access for home %d: %v", homeID, err)
		}
	}

	for _, device := range devices {
		mqtt.UnsubscribeDevice(device)
		if err := mqtt.ClearRetainedDeviceFirmwareUpdate(device); err != nil {
			log.Printf("Failed clearing retained ota for deleted device %d: %v", device.ID, err)
		}
	}
	deviceIDs := make([]uint, 0, len(devices))
	for _, device := range devices {
		deviceIDs = append(deviceIDs, device.ID)
	}
	state.RemoveDevices(deviceIDs)

	c.JSON(http.StatusOK, gin.H{
		"deleted":    true,
		"home_id":    home.ID,
		"device_ids": deviceIDs,
	})
}

func deleteDevice(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	deviceIDValue, err := strconv.ParseUint(c.Param("deviceID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	deviceID := uint(deviceIDValue)

	var device models.Device
	if err := db.DB.First(&device, deviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	if device.UserID != sessionUser.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "device does not belong to user"})
		return
	}

	if err := db.DB.Unscoped().Delete(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete device"})
		return
	}

	mqtt.UnsubscribeDevice(device)
	if err := mqtt.ClearRetainedDeviceFirmwareUpdate(device); err != nil {
		log.Printf("Failed clearing retained ota for deleted device %d: %v", device.ID, err)
	}
	state.RemoveDevice(device.ID)

	c.JSON(http.StatusOK, gin.H{
		"deleted":   true,
		"device_id": device.ID,
	})
}

func enrollHome(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Name         string `json:"name" binding:"required"`
		WiFiSSID     string `json:"wifi_ssid"`
		WiFiPassword string `json:"wifi_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var (
		home        models.Home
		mqttUser    string
		mqttPass    string
		provisioned bool
	)

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		home = models.Home{
			UserID:       sessionUser.UserID,
			Name:         req.Name,
			WiFiSSID:     req.WiFiSSID,
			WiFiPassword: req.WiFiPassword,
		}
		if err := tx.Create(&home).Error; err != nil {
			return fmt.Errorf("create home row: %w", err)
		}

		var err error
		mqttUser, err = generateHomeMQTTUsername(sessionUser.UserID, home.ID)
		if err != nil {
			return fmt.Errorf("generate mqtt username: %w", err)
		}
		mqttPass, err = generateHomeMQTTPassword()
		if err != nil {
			return fmt.Errorf("generate mqtt password: %w", err)
		}

		if err := mqtt.ProvisionHomeAccess(sessionUser.UserID, home.ID, mqttUser, mqttPass); err != nil {
			return fmt.Errorf("provision mqtt access: %w", err)
		}
		provisioned = true

		if err := tx.Model(&home).Updates(map[string]interface{}{
			"mqtt_username": mqttUser,
			"mqtt_password": mqttPass,
		}).Error; err != nil {
			return fmt.Errorf("persist mqtt credentials: %w", err)
		}

		home.MQTTUsername = mqttUser
		home.MQTTPassword = mqttPass
		return nil
	})
	if err != nil {
		if provisioned {
			if cleanupErr := mqtt.CleanupHomeAccess(sessionUser.UserID, home.ID, mqttUser); cleanupErr != nil {
				log.Printf("Failed to clean up mqtt access for home %d: %v", home.ID, cleanupErr)
			}
		}
		log.Printf("Failed to create home: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create home"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"home_id": home.ID,
		"name":    home.Name,
	})
}

func enrollDevice(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		HomeID         uint   `json:"home_id" binding:"required"`
		Name           string `json:"name" binding:"required"`
		ProductID      uint   `json:"product_id" binding:"required"`
		ProductName    string `json:"product_name" binding:"required"`
		DevicePublicKey string `json:"device_public_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	devicePubKey, err := devcrypto.ParseDevicePublicKey(req.DevicePublicKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device public key"})
		return
	}

	var home models.Home
	if err := db.DB.First(&home, req.HomeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "home not found"})
		return
	}
	if home.UserID != sessionUser.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "home does not belong to user"})
		return
	}

	var product models.Product
	if err := db.DB.Where("id = ? AND name = ?", req.ProductID, req.ProductName).Take(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load product"})
		return
	}

	device := models.Device{
		UserID:          sessionUser.UserID,
		HomeID:          req.HomeID,
		ProductID:       product.ID,
		Name:            req.Name,
		DevicePublicKey: req.DevicePublicKey,
	}
	if err := db.DB.Create(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create device"})
		return
	}

	state.CacheDevice(state.DeviceInfo{
		ID: device.ID, UserID: device.UserID, HomeID: device.HomeID,
		ProductID: device.ProductID,
	})

	mqtt.SubscribeDevice(device)

	mqttHost, mqttPort := cfg.MQTTHostAndPort()
	bundle, err := devcrypto.EncryptProvisioningBundle(devicePubKey, devcrypto.ProvisioningPayload{
		DeviceID:     fmt.Sprintf("%d", device.ID),
		UserID:       fmt.Sprintf("%d", sessionUser.UserID),
		HomeID:       fmt.Sprintf("%d", req.HomeID),
		MQTTHost:     mqttHost,
		MQTTPort:     mqttPort,
		MQTTUsername: home.MQTTUsername,
		MQTTPassword: home.MQTTPassword,
		WiFiSSID:     home.WiFiSSID,
		WiFiPassword: home.WiFiPassword,
	})
	if err != nil {
		log.Printf("Failed to encrypt provisioning bundle for device %d: %v", device.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt provisioning data"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"device_id":           fmt.Sprintf("%d", device.ID),
		"provisioning_bundle": bundle,
	})
}
