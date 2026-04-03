package google

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/oauth"
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
	api := r.Group("/api/google")
	api.Use(oauth.RequireOAuthToken())
	api.POST("/fulfillment", func(c *gin.Context) {
		principal, ok := oauth.CurrentOAuthPrincipal(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		var req GoogleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
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

		var payload interface{}
		switch intent {
		case "action.devices.SYNC":
			payload = handleSync(principal.RawUserID, principal.UserID)
		case "action.devices.QUERY":
			queryPayload, err := handleQuery(req.Inputs[0].Payload, principal.UserID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid query payload"})
				return
			}
			payload = queryPayload
		case "action.devices.EXECUTE":
			execPayload, err := handleExecute(req.Inputs[0].Payload, principal.UserID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execute payload"})
				return
			}
			payload = execPayload
		case "action.devices.DISCONNECT":
			c.JSON(http.StatusOK, gin.H{})
			return
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown intent"})
			return
		}

		res.Payload = payload
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

// handleSync reads only the current user's device metadata from Valkey and
// batches the remaining work so SYNC doesn't fan out into per-device cache hits.
func handleSync(agentUserID string, userID uint) interface{} {
	devices := state.GetDevicesForUser(userID)

	productCaps := make(map[uint][]state.CapInfo)
	var googleDevices []map[string]interface{}
	for _, d := range devices {
		caps, ok := productCaps[d.ProductID]
		if !ok {
			loadedCaps, err := state.GetProductCaps(d.ProductID)
			if err != nil {
				productCaps[d.ProductID] = nil
				continue
			}
			productCaps[d.ProductID] = loadedCaps
			caps = loadedCaps
		}

		deviceName := d.Name
		for _, cap := range caps {
			compoundID := fmt.Sprintf("%d:%s", d.ID, cap.EsphomeKey)
			traitName := "action.devices.traits." + cap.TraitType

			dev := map[string]interface{}{
				"id":     compoundID,
				"type":   cap.GoogleDeviceType,
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
		"agentUserId": agentUserID,
		"devices":     googleDevices,
	}
}

// handleQuery reads device metadata and state entirely from Valkey.
func handleQuery(payload json.RawMessage, userID uint) (interface{}, error) {
	var queryPayload struct {
		Devices []struct {
			ID string `json:"id"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(payload, &queryPayload); err != nil {
		return nil, err
	}

	devicesMap := make(map[string]interface{})

	for _, d := range queryPayload.Devices {
		deviceID, capKey, err := parseCompoundID(d.ID)
		if err != nil {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR", "errorCode": "deviceNotFound"}
			continue
		}

		device, ok := state.GetDevice(deviceID)
		if !ok || device.UserID != userID {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR", "errorCode": "deviceNotFound"}
			continue
		}

		capInfo, ok := state.GetProductCapByKey(device.ProductID, capKey)
		if !ok {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR", "errorCode": "deviceNotFound"}
			continue
		}

		val, err := state.GetCapState(deviceID, capKey)
		if err != nil {
			devicesMap[d.ID] = map[string]interface{}{"status": "ERROR", "errorCode": "deviceOffline"}
			continue
		}

		stateMap := buildTraitState(capInfo.TraitType, val)
		stateMap["status"] = "SUCCESS"
		stateMap["online"] = true
		devicesMap[d.ID] = stateMap
	}

	return map[string]interface{}{
		"devices": devicesMap,
	}, nil
}

// handleExecute reads device metadata from Valkey, publishes to MQTT, updates state in Valkey.
func handleExecute(payload json.RawMessage, userID uint) (interface{}, error) {
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
	if err := json.Unmarshal(payload, &execPayload); err != nil {
		return nil, err
	}

	var commandsResult []map[string]interface{}

	for _, cmd := range execPayload.Commands {
		for _, d := range cmd.Devices {
			compoundID := d.ID
			for _, execution := range cmd.Execution {
				deviceID, capKey, err := parseCompoundID(compoundID)
				if err != nil {
					commandsResult = append(commandsResult, map[string]interface{}{
						"ids":       []string{compoundID},
						"status":    "ERROR",
						"errorCode": "deviceNotFound",
					})
					continue
				}

				device, ok := state.GetDevice(deviceID)
				if !ok || device.UserID != userID {
					commandsResult = append(commandsResult, map[string]interface{}{
						"ids":       []string{compoundID},
						"status":    "ERROR",
						"errorCode": "deviceNotFound",
					})
					continue
				}

				capInfo, ok := state.GetProductCapByKey(device.ProductID, capKey)
				if !ok || !capInfo.Writable {
					commandsResult = append(commandsResult, map[string]interface{}{
						"ids":       []string{compoundID},
						"status":    "ERROR",
						"errorCode": "functionNotSupported",
					})
					continue
				}

				mqttPayload, supported := extractMQTTPayload(execution.Command, execution.Params)
				if !supported {
					commandsResult = append(commandsResult, map[string]interface{}{
						"ids":       []string{compoundID},
						"status":    "ERROR",
						"errorCode": "functionNotSupported",
					})
					continue
				}
				state.SetCapState(deviceID, capKey, mqttPayload)

				topic := models.BuildTopic(device.UserID, device.HomeID, deviceID, capInfo.Component, capKey, "command")
				mqtt.Publish(topic, mqttPayload)
				log.Printf("Google Execute: %s cap %s = %s", compoundID, capKey, mqttPayload)

				commandsResult = append(commandsResult, map[string]interface{}{
					"ids":    []string{compoundID},
					"status": "SUCCESS",
					"states": map[string]interface{}{"online": true},
				})
			}
		}
	}

	return map[string]interface{}{
		"commands": commandsResult,
	}, nil
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

func extractMQTTPayload(command string, params map[string]interface{}) (string, bool) {
	switch command {
	case "action.devices.commands.OnOff":
		on, _ := params["on"].(bool)
		if on {
			return "1", true
		}
		return "0", true
	default:
		return "", false
	}
}
