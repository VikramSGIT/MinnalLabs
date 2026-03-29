package admin

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
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
			"product_id":        product.ID,
			"name":              product.Name,
			"firmware_version":  product.FirmwareVersion,
			"firmware_filename": product.FirmwareFilename,
			"firmware_md5":      product.FirmwareMD5,
		}
		if product.FirmwareFilename != "" {
			item["firmware_url"] = cfg.FirmwareFileURL(product.FirmwareFilename)
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

func buildFirmwareFilename(productID uint, version string) string {
	return fmt.Sprintf("%d_%s.bin", productID, version)
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

	var product models.Product
	if err := db.DB.First(&product, productID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	if err := os.MkdirAll(cfg.FirmwareStoragePath(), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create firmware storage"})
		return
	}

	filename := buildFirmwareFilename(product.ID, version)
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

	now := time.Now().UTC()
	if err := db.DB.Model(&product).Updates(map[string]interface{}{
		"firmware_version":     version,
		"firmware_filename":    filename,
		"firmware_md5":         md5sum,
		"firmware_uploaded_at": now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist firmware metadata"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":           product.ID,
		"name":                 product.Name,
		"firmware_version":     version,
		"firmware_filename":    filename,
		"firmware_md5":         md5sum,
		"firmware_url":         cfg.FirmwareFileURL(filename),
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
	if product.FirmwareFilename == "" || product.FirmwareVersion == "" || product.FirmwareMD5 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "upload firmware before triggering rollout"})
		return
	}

	if req.BatchPercentage < 1 || req.BatchPercentage > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "batch_percentage must be between 1 and 100"})
		return
	}
	if req.BatchIntervalValue < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "batch_interval_value must be at least 1"})
		return
	}

	unit := strings.ToLower(strings.TrimSpace(req.BatchIntervalUnit))
	batchIntervalMinutes := 0
	switch unit {
	case "hour", "hours":
		batchIntervalMinutes = req.BatchIntervalValue * 60
	case "day", "days":
		batchIntervalMinutes = req.BatchIntervalValue * 24 * 60
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "batch_interval_unit must be hours or days"})
		return
	}

	rollout, eligibleDevices, totalBatches, err := ota.CreateRollout(product, sessionUser.UserID, req.BatchPercentage, batchIntervalMinutes)
	if err != nil {
		if strings.Contains(err.Error(), "no eligible devices") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"rollout_id":             rollout.ID,
		"product_id":             product.ID,
		"firmware_version":       product.FirmwareVersion,
		"firmware_url":           cfg.FirmwareFileURL(product.FirmwareFilename),
		"batch_percentage":       rollout.BatchPercentage,
		"batch_interval_minutes": rollout.BatchIntervalMinutes,
		"eligible_devices":       eligibleDevices,
		"total_batches":          totalBatches,
		"status":                 rollout.Status,
		"next_batch_at":          rollout.NextBatchAt,
	})
}
