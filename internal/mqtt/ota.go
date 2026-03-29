package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/iot-backend/internal/models"
)

type OTACommand struct {
	ProductID   uint   `json:"product_id"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	MD5         string `json:"md5"`
	RolloutID   uint   `json:"rollout_id,omitempty"`
	BatchNumber int    `json:"batch_number,omitempty"`
	IssuedAt    string `json:"issued_at"`
}

func publishJSON(topic string, payload interface{}, retain bool) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal mqtt payload: %w", err)
	}
	token := Client.Publish(topic, 1, retain, data)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("publish mqtt payload: %w", err)
	}
	log.Printf("Published JSON to topic: %s", topic)
	return nil
}

func PublishRetainedDeviceFirmwareUpdate(device models.Device, version, url, md5 string, rolloutID uint, batchNumber int) error {
	payload := OTACommand{
		ProductID:   device.ProductID,
		Version:     version,
		URL:         url,
		MD5:         md5,
		RolloutID:   rolloutID,
		BatchNumber: batchNumber,
		IssuedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	topic := models.BuildOTACommandTopic(device.UserID, device.HomeID, device.ID)
	return publishJSON(topic, payload, true)
}

func ClearRetainedDeviceFirmwareUpdate(device models.Device) error {
	topic := models.BuildOTACommandTopic(device.UserID, device.HomeID, device.ID)
	token := Client.Publish(topic, 1, true, []byte{})
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("clear retained ota command: %w", err)
	}
	log.Printf("Cleared retained OTA command for topic: %s", topic)
	return nil
}

func PublishDeviceFirmwareUpdateNow(device models.Device, version, url, md5 string) error {
	return PublishRetainedDeviceFirmwareUpdate(device, version, url, md5, 0, 0)
}
