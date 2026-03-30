package google

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/state"
)

type GoogleRequest struct {
	RequestId string `json:"requestId"`
	Inputs    []struct {
		Intent  string          `json:"intent"`
		Payload json.RawMessage `json:"payload"`
	} `json:"inputs"`
}

type GoogleResponse struct {
	RequestId string      `json:"requestId"`
	Payload   interface{} `json:"payload"`
}

func SetupGoogleRoutes(r *gin.Engine) {
	r.POST("/api/google/fulfillment", func(c *gin.Context) {
		var req GoogleRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}

		if len(req.Inputs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no inputs"})
			return
		}

		intent := req.Inputs[0].Intent
		log.Printf("Received Google intent: %s", intent)

		var res GoogleResponse
		res.RequestId = req.RequestId

		switch intent {
		case "action.devices.SYNC":
			res.Payload = handleSync()
		case "action.devices.QUERY":
			res.Payload = handleQuery(req.Inputs[0].Payload)
		case "action.devices.EXECUTE":
			res.Payload = handleExecute(req.Inputs[0].Payload)
		case "action.devices.DISCONNECT":
			c.JSON(http.StatusOK, gin.H{})
			return
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown intent"})
			return
		}

		c.JSON(http.StatusOK, res)
	})
}

func parseCompoundID(compoundID string) (uint, string, error) {
	parts := strings.SplitN(compoundID, ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid compound ID: %s", compoundID)
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid device ID in compound ID: %s", compoundID)
	}
	return uint(id), parts[1], nil
}

// handleSync reads device IDs and product info from Valkey, names from DB (SYNC is infrequent).
func handleSync() interface{} {
	devices := state.GetAllDevices()

	deviceIDs := make([]uint, 0, len(devices))
	for _, d := range devices {
		deviceIDs = append(deviceIDs, d.ID)
	}

	nameMap := make(map[uint]string, len(deviceIDs))
	if len(deviceIDs) > 0 {
		var dbDevices []models.Device
		db.DB.Select("id, name").Where("id IN ?", deviceIDs).Find(&dbDevices)
		for _, dev := range dbDevices {
			nameMap[dev.ID] = dev.Name
		}
	}

	var googleDevices []map[string]interface{}
	for _, d := range devices {
		caps, err := state.GetProductCaps(d.ProductID)
		if err != nil {
			continue
		}
		deviceName := nameMap[d.ID]
		for _, cap := range caps {
			compoundID := fmt.Sprintf("%d:%s", d.ID, cap.EsphomeKey)
			traitName := "action.devices.traits." + cap.TraitType

			dev := map[string]interface{}{
				"id":   compoundID,
				"type": cap.GoogleDeviceType,
				"traits": []string{traitName},
				"name": map[string]interface{}{
					"name": fmt.Sprintf("%s %s", deviceName, cap.EsphomeKey),
				},
				"willReportState": true,
			}
			googleDevices = append(googleDevices, dev)
		}
	}

	return map[string]interface{}{
		"agentUserId": "user123",
		"devices":     googleDevices,
	}
}

// handleQuery reads device metadata and state entirely from Valkey.
func handleQuery(payload json.RawMessage) interface{} {
	var queryPayload struct {
		Devices []struct {
			ID string `json:"id"`
		} `json:"devices"`
	}
	json.Unmarshal(payload, &queryPayload)

	devicesMap := make(map[string]interface{})

	for _, d := range queryPayload.Devices {
		deviceID, capKey, err := parseCompoundID(d.ID)
		if err != nil {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR"}
			continue
		}

		device, ok := state.GetDevice(deviceID)
		if !ok {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR"}
			continue
		}

		capInfo, ok := state.GetProductCapByKey(device.ProductID, capKey)
		if !ok {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR"}
			continue
		}

		val, err := state.GetCapState(deviceID, capKey)
		if err != nil {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR"}
			continue
		}

		stateMap := buildTraitState(capInfo.TraitType, val)
		stateMap["status"] = "SUCCESS"
		stateMap["online"] = true
		devicesMap[d.ID] = stateMap
	}

	return map[string]interface{}{
		"devices": devicesMap,
	}
}

// handleExecute reads device metadata from Valkey, publishes to MQTT, updates state in Valkey.
func handleExecute(payload json.RawMessage) interface{} {
	var execPayload struct {
		Commands []struct {
			Devices []struct {
				ID string `json:"id"`
			} `json:"devices"`
			Execution []struct {
				Command string                 `json:"command"`
				Params  map[string]interface{} `json:"params"`
			} `json:"execution"`
		} `json:"commands"`
	}
	json.Unmarshal(payload, &execPayload)

	var commandsResult []map[string]interface{}

	for _, cmd := range execPayload.Commands {
		var ids []string
		for _, d := range cmd.Devices {
			ids = append(ids, d.ID)
		}

		for _, execution := range cmd.Execution {
			for _, compoundID := range ids {
				deviceID, capKey, err := parseCompoundID(compoundID)
				if err != nil {
					continue
				}

				device, ok := state.GetDevice(deviceID)
				if !ok {
					continue
				}

				capInfo, ok := state.GetProductCapByKey(device.ProductID, capKey)
				if !ok || !capInfo.Writable {
					continue
				}

				mqttPayload := extractMQTTPayload(execution.Command, execution.Params)
				state.SetCapState(deviceID, capKey, mqttPayload)

				topic := models.BuildTopic(device.UserID, device.HomeID, deviceID, capInfo.Component, capKey, "command")
				mqtt.Publish(topic, mqttPayload)
				log.Printf("Google Execute: %s cap %s = %s", compoundID, capKey, mqttPayload)
			}
		}

		commandsResult = append(commandsResult, map[string]interface{}{
			"ids":    ids,
			"status": "SUCCESS",
			"states": map[string]interface{}{"online": true},
		})
	}

	return map[string]interface{}{
		"commands": commandsResult,
	}
}

func buildTraitState(traitType, value string) map[string]interface{} {
	m := make(map[string]interface{})
	switch traitType {
	case "OnOff":
		m["on"] = value == "1" || strings.EqualFold(value, "on") || strings.EqualFold(value, "true")
	case "MotionDetection":
		m["on"] = value == "1" || strings.EqualFold(value, "on") || strings.EqualFold(value, "true")
	default:
		m["on"] = value == "1"
	}
	return m
}

func extractMQTTPayload(command string, params map[string]interface{}) string {
	switch command {
	case "action.devices.commands.OnOff":
		on, _ := params["on"].(bool)
		if on {
			return "1"
		}
		return "0"
	default:
		return "0"
	}
}
