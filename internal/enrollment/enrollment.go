package enrollment

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/state"
	"golang.org/x/crypto/bcrypt"
)

var cfg *config.Config

func SetupEnrollmentRoutes(r *gin.Engine, appCfg *config.Config) {
	cfg = appCfg

	api := r.Group("/api/enroll")
	{
		api.POST("/user", enrollUser)
		api.POST("/home", enrollHome)
		api.POST("/device", enrollDevice)
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

func enrollHome(c *gin.Context) {
	var req struct {
		UserID       uint   `json:"user_id" binding:"required"`
		Name         string `json:"name" binding:"required"`
		WiFiSSID     string `json:"wifi_ssid"`
		WiFiPassword string `json:"wifi_password"`
		MQTTUsername string `json:"mqtt_username"`
		MQTTPassword string `json:"mqtt_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	if err := db.DB.First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	home := models.Home{
		UserID:       req.UserID,
		Name:         req.Name,
		WiFiSSID:     req.WiFiSSID,
		WiFiPassword: req.WiFiPassword,
		MQTTUsername: req.MQTTUsername,
		MQTTPassword: req.MQTTPassword,
	}
	if err := db.DB.Create(&home).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create home"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"home_id": home.ID,
		"name":    home.Name,
	})
}

func enrollDevice(c *gin.Context) {
	var req struct {
		UserID      uint   `json:"user_id" binding:"required"`
		HomeID      uint   `json:"home_id" binding:"required"`
		Name        string `json:"name" binding:"required"`
		ProductName string `json:"product_name" binding:"required"`
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
	if home.UserID != req.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "home does not belong to user"})
		return
	}

	var product models.Product
	if err := db.DB.Where("name = ?", req.ProductName).First(&product).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	device := models.Device{
		UserID:    req.UserID,
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

	c.JSON(http.StatusCreated, gin.H{
		"device_id":     fmt.Sprintf("%d", device.ID),
		"user_id":       fmt.Sprintf("%d", req.UserID),
		"home_id":       fmt.Sprintf("%d", req.HomeID),
		"mqtt_url":      cfg.MQTT.Broker,
		"mqtt_port":     "1883",
		"mqtt_username": home.MQTTUsername,
		"mqtt_password": home.MQTTPassword,
		"wifi_ssid":     home.WiFiSSID,
		"wifi_password": home.WiFiPassword,
	})
}
