package admin

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/oauth"
	"github.com/iot-backend/internal/ota"
)

var cfg *config.Config

var versionSanitizer = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func SetupAdminRoutes(r *gin.Engine, appCfg *config.Config) {
	cfg = appCfg

	api := r.Group("/api/admin")
	api.Use(oauth.RequireSession(), oauth.RequireAdmin())
	{
		api.GET("/products", listProducts)
		api.GET("/products/:productID/rollouts", listProductRollouts)
		api.POST("/products/:productID/firmware", uploadFirmware)
		api.POST("/products/:productID/rollout", rolloutFirmware)
	}
}

func listProducts(c *gin.Context) {
	var products []models.Product
	if err := db.DB.Order("id ASC").Find(&products).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load products"})
		return
	}

	response := make([]gin.H, 0, len(products))
	for _, product := range products {
		item := gin.H{
			"product_id":               product.ID,
			"name":                     product.Name,
			"firmware_version":         product.FirmwareVersion,
			"firmware_url":             effectiveFirmwareURL(product),
			"firmware_md5_url":         effectiveFirmwareMD5URL(product),
			"rollout_percentage":       product.EffectiveRolloutPercentage(),
			"rollout_interval_minutes": product.EffectiveRolloutIntervalMinutes(),
		}
		if product.FirmwareUploadedAt != nil {
			item["firmware_uploaded_at"] = product.FirmwareUploadedAt
		}
		response = append(response, item)
	}

	c.JSON(http.StatusOK, response)
}

func parseProductID(c *gin.Context) (uint, bool) {
	productIDValue, err := strconv.ParseUint(c.Param("productID"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return 0, false
	}
	return uint(productIDValue), true
}

func sanitizeVersion(version string) string {
	return versionSanitizer.ReplaceAllString(strings.TrimSpace(version), "_")
}

func effectiveFirmwareURL(product models.Product) string {
	return models.DeriveFirmwareURL(product.FirmwareURL, product.ID, product.FirmwareVersion)
}

func effectiveFirmwareMD5URL(product models.Product) string {
	return models.DeriveFirmwareMD5URL(product.FirmwareURL, product.FirmwareMD5URL, product.ID, product.FirmwareVersion)
}

func normalizeBatchIntervalMinutes(value int, unit string) (int, error) {
	if value < 1 {
		return 0, fmt.Errorf("batch_interval_value must be at least 1")
	}

	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "hour", "hours":
		return value * 60, nil
	case "day", "days":
		return value * 24 * 60, nil
	default:
		return 0, fmt.Errorf("batch_interval_unit must be hours or days")
	}
}

func validateFirmwareUploadName(filename string) error {
	lowerName := strings.ToLower(strings.TrimSpace(filename))
	if lowerName == "" {
		return fmt.Errorf("firmware file name is required")
	}
	if strings.HasSuffix(lowerName, ".factory.bin") {
		return fmt.Errorf("upload the ESPHome OTA binary (*.ota.bin), not the factory binary (*.factory.bin)")
	}
	return nil
}

func fileMD5(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func uploadFirmware(c *gin.Context) {
	productID, ok := parseProductID(c)
	if !ok {
		return
	}

	version := sanitizeVersion(c.PostForm("version"))
	if version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version is required"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "firmware file is required"})
		return
	}
	if err := validateFirmwareUploadName(fileHeader.Filename); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var product models.Product
	if err := db.DB.First(&product, productID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	if err := os.MkdirAll(cfg.FirmwareStoragePath(), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create firmware storage"})
		return
	}

	filename := models.BuildFirmwareFilename(product.ID, version)
	path := filepath.Join(cfg.FirmwareStoragePath(), filename)
	if err := c.SaveUploadedFile(fileHeader, path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save firmware file"})
		return
	}

	md5sum, err := fileMD5(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash firmware file"})
		return
	}

	md5Filename := models.BuildFirmwareMD5Filename(product.ID, version)
	md5Path := filepath.Join(cfg.FirmwareStoragePath(), md5Filename)
	if err := os.WriteFile(md5Path, []byte(md5sum), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write firmware md5 file"})
		return
	}

	now := time.Now().UTC()
	if err := db.DB.Model(&product).Updates(map[string]interface{}{
		"firmware_version":     version,
		"firmware_uploaded_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist firmware metadata"})
		return
	}
	product.FirmwareVersion = version
	product.FirmwareUploadedAt = &now

	c.JSON(http.StatusOK, gin.H{
		"product_id":           product.ID,
		"name":                 product.Name,
		"firmware_version":     version,
		"firmware_url":         effectiveFirmwareURL(product),
		"firmware_md5_url":     effectiveFirmwareMD5URL(product),
		"firmware_uploaded_at": now,
	})
}

func listProductRollouts(c *gin.Context) {
	productID, ok := parseProductID(c)
	if !ok {
		return
	}

	rollouts, err := ota.ListRolloutsForProduct(productID, 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rollouts"})
		return
	}

	c.JSON(http.StatusOK, rollouts)
}

func rolloutFirmware(c *gin.Context) {
	productID, ok := parseProductID(c)
	if !ok {
		return
	}

	sessionUser, ok := oauth.CurrentSessionUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		BatchPercentage    int    `json:"batch_percentage" binding:"required"`
		BatchIntervalValue int    `json:"batch_interval_value" binding:"required"`
		BatchIntervalUnit  string `json:"batch_interval_unit" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var product models.Product
	if err := db.DB.First(&product, productID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	if effectiveFirmwareURL(product) == "" || product.FirmwareVersion == "" || effectiveFirmwareMD5URL(product) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "upload firmware before triggering rollout"})
		return
	}

	if req.BatchPercentage < 1 || req.BatchPercentage > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "batch_percentage must be between 1 and 100"})
		return
	}
	batchIntervalMinutes, err := normalizeBatchIntervalMinutes(req.BatchIntervalValue, req.BatchIntervalUnit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := db.DB.Model(&product).Updates(map[string]interface{}{
		"rollout_percentage":       req.BatchPercentage,
		"rollout_interval_minutes": batchIntervalMinutes,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist rollout settings"})
		return
	}
	product.RolloutPercentage = req.BatchPercentage
	product.RolloutIntervalMinutes = batchIntervalMinutes

	rollout, eligibleDevices, totalBatches, err := ota.CreateRollout(product, sessionUser.UserID)
	if err != nil {
		if strings.Contains(err.Error(), "no eligible devices") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		log.Printf("Failed to create rollout for product %d: %v", productID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rollout"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"rollout_id":             rollout.ID,
		"product_id":             product.ID,
		"firmware_version":       product.FirmwareVersion,
		"firmware_url":           effectiveFirmwareURL(product),
		"firmware_md5_url":       effectiveFirmwareMD5URL(product),
		"batch_percentage":       rollout.BatchPercentage,
		"batch_interval_minutes": rollout.BatchIntervalMinutes,
		"eligible_devices":       eligibleDevices,
		"total_batches":          totalBatches,
		"status":                 rollout.Status,
		"next_batch_at":          rollout.NextBatchAt,
	})
}
