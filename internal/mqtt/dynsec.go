package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	dynsecControlTopic  = "$CONTROL/dynamic-security/v1"
	dynsecResponseTopic = "$CONTROL/dynamic-security/v1/response"
	dynsecTimeout       = 5 * time.Second
)

var (
	dynsecMu     sync.Mutex
	dynsecRespCh = make(chan dynsecEnvelope, 64)
)

type dynsecEnvelope struct {
	Responses []dynsecResponse `json:"responses"`
}

type dynsecResponse struct {
	Command string `json:"command"`
	Error   string `json:"error"`
}

func subscribeDynsecResponses() {
	token := Client.Subscribe(dynsecResponseTopic, 1, dynsecResponseHandler)
	token.Wait()
	if err := token.Error(); err != nil {
		log.Fatalf("Error subscribing to Dynamic Security responses: %v", err)
	}
}

var dynsecResponseHandler pahomqtt.MessageHandler = func(client pahomqtt.Client, msg pahomqtt.Message) {
	var envelope dynsecEnvelope
	if err := json.Unmarshal(msg.Payload(), &envelope); err != nil {
		log.Printf("Failed to decode Dynamic Security response: %v", err)
		return
	}

	select {
	case dynsecRespCh <- envelope:
	default:
		log.Printf("Dropping Dynamic Security response because the queue is full")
	}
}

func clearDynsecResponses() {
	for {
		select {
		case <-dynsecRespCh:
		default:
			return
		}
	}
}

func sendDynsecCommand(command map[string]interface{}) error {
	return sendDynsecCommands([]map[string]interface{}{command})
}

func sendDynsecCommands(commands []map[string]interface{}) error {
	if len(commands) == 0 {
		return nil
	}

	dynsecMu.Lock()
	defer dynsecMu.Unlock()

	clearDynsecResponses()

	payload, err := json.Marshal(map[string]interface{}{
		"commands": commands,
	})
	if err != nil {
		return fmt.Errorf("marshal dynsec commands: %w", err)
	}

	token := Client.Publish(dynsecControlTopic, 1, false, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("publish dynsec commands: %w", err)
	}

	select {
	case envelope := <-dynsecRespCh:
		if len(envelope.Responses) == 0 {
			return fmt.Errorf("empty dynsec response")
		}
		for _, response := range envelope.Responses {
			if strings.TrimSpace(response.Error) != "" {
				return fmt.Errorf("%s: %s", response.Command, response.Error)
			}
		}
		return nil
	case <-time.After(dynsecTimeout):
		return fmt.Errorf("timed out waiting for dynsec response")
	}
}

func ignoreMissingDynsecError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "not found") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "unknown client") ||
		strings.Contains(message, "unknown role")
}

func homeRoleName(userID, homeID uint) string {
	return fmt.Sprintf("home_%d_%d", userID, homeID)
}

func homeTopicPattern(userID, homeID uint) string {
	return fmt.Sprintf("%d/%d/#", userID, homeID)
}

func ProvisionHomeAccess(userID, homeID uint, username, password string) error {
	roleName := homeRoleName(userID, homeID)
	topicPattern := homeTopicPattern(userID, homeID)

	commands := []map[string]interface{}{
		{
			"command":  "createRole",
			"rolename": roleName,
		},
		{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  "publishClientSend",
			"topic":    topicPattern,
			"allow":    true,
			"priority": -1,
		},
		{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  "publishClientReceive",
			"topic":    topicPattern,
			"allow":    true,
			"priority": -1,
		},
		{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  "subscribePattern",
			"topic":    topicPattern,
			"allow":    true,
			"priority": -1,
		},
		{
			"command":  "createClient",
			"username": username,
			"password": password,
		},
		{
			"command":  "addClientRole",
			"username": username,
			"rolename": roleName,
			"priority": -1,
		},
	}

	if err := sendDynsecCommands(commands); err != nil {
		// Attempt cleanup on failure
		_ = sendDynsecCommand(map[string]interface{}{
			"command":  "deleteClient",
			"username": username,
		})
		_ = sendDynsecCommand(map[string]interface{}{
			"command":  "deleteRole",
			"rolename": roleName,
		})
		return err
	}

	return nil
}

func CleanupHomeAccess(userID, homeID uint, username string) error {
	roleName := homeRoleName(userID, homeID)

	commands := make([]map[string]interface{}, 0, 2)
	if strings.TrimSpace(username) != "" {
		commands = append(commands, map[string]interface{}{
			"command":  "deleteClient",
			"username": username,
		})
	}
	commands = append(commands, map[string]interface{}{
		"command":  "deleteRole",
		"rolename": roleName,
	})

	err := sendDynsecCommands(commands)
	if err != nil && !ignoreMissingDynsecError(err) {
		return err
	}

	return nil
}
