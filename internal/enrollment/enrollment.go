package enrollment

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/oauth"
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
		protected.GET("/device/:deviceID/status", getDeviceStatus)
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
		Where("home_id = ?", homeID).
		Order("created_at DESC, id DESC").
		Find(&devices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load devices"})
		return
	}

	response := make([]gin.H, 0, len(devices))
	for _, device := range devices {
		item := gin.H{
			"device_id":      device.ID,
			"name":           device.Name,
			"product_id":     device.ProductID,
			"mqtt_connected": false,
			"mqtt_status":    "unknown",
			"created_at":     device.CreatedAt,
		}

		if presence, found := state.GetDevicePresence(device.ID); found {
			item["mqtt_connected"] = presence.Online
			item["mqtt_status"] = presence.LastStatus
			if !presence.LastStatusAt.IsZero() {
				item["last_status_at"] = presence.LastStatusAt
			}
			if !presence.LastSeenAt.IsZero() {
				item["last_seen_at"] = presence.LastSeenAt
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
	if err := db.DB.Select("id, user_id").First(&device, deviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	if device.UserID != sessionUser.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "device does not belong to user"})
		return
	}

	presence, found := state.GetDevicePresence(deviceID)
	if !found {
		c.JSON(http.StatusOK, gin.H{
			"device_id":      deviceID,
			"mqtt_connected": false,
			"mqtt_status":    "unknown",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"device_id":      deviceID,
		"mqtt_connected": presence.Online,
		"mqtt_status":    presence.LastStatus,
		"last_status_at": presence.LastStatusAt,
		"last_seen_at":   presence.LastSeenAt,
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

	var user models.User
	if err := db.DB.First(&user, sessionUser.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	mqttHost, mqttPort := cfg.MQTTHostAndPort()
	c.JSON(http.StatusCreated, gin.H{
		"home_id":       home.ID,
		"name":          home.Name,
		"mqtt_host":     mqttHost,
		"mqtt_port":     mqttPort,
		"mqtt_username": home.MQTTUsername,
		"mqtt_password": home.MQTTPassword,
	})
}

func enrollDevice(c *gin.Context) {
	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		HomeID    uint   `json:"home_id" binding:"required"`
		Name      string `json:"name" binding:"required"`
		ProductID uint   `json:"product_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	if err := db.DB.First(&product, req.ProductID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	device := models.Device{
		UserID:    sessionUser.UserID,
		HomeID:    req.HomeID,
		ProductID: product.ID,
		Name:      req.Name,
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
	c.JSON(http.StatusCreated, gin.H{
		"device_id":     fmt.Sprintf("%d", device.ID),
		"user_id":       fmt.Sprintf("%d", sessionUser.UserID),
		"home_id":       fmt.Sprintf("%d", req.HomeID),
		"mqtt_host":     mqttHost,
		"mqtt_port":     mqttPort,
		"mqtt_username": home.MQTTUsername,
		"mqtt_password": home.MQTTPassword,
		"wifi_ssid":     home.WiFiSSID,
		"wifi_password": home.WiFiPassword,
	})
}
