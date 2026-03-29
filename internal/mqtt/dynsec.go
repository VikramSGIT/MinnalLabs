package mqtt

import (
	"encoding/json"
	"errors"
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
	dynsecRespCh = make(chan dynsecEnvelope, 8)
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
	dynsecMu.Lock()
	defer dynsecMu.Unlock()

	clearDynsecResponses()

	payload, err := json.Marshal(map[string]interface{}{
		"commands": []map[string]interface{}{command},
	})
	if err != nil {
		return fmt.Errorf("marshal dynsec command: %w", err)
	}

	token := Client.Publish(dynsecControlTopic, 1, false, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("publish dynsec command: %w", err)
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

func homeRoleName(userID, homeID uint) string {
	return fmt.Sprintf("home_%d_%d", userID, homeID)
}

func homeTopicPattern(userID, homeID uint) string {
	return fmt.Sprintf("%d/%d/#", userID, homeID)
}

func ProvisionHomeAccess(userID, homeID uint, username, password string) (err error) {
	roleName := homeRoleName(userID, homeID)
	topicPattern := homeTopicPattern(userID, homeID)
	roleCreated := false
	clientCreated := false

	defer func() {
		if err == nil {
			return
		}
		if clientCreated {
			if cleanupErr := sendDynsecCommand(map[string]interface{}{
				"command":  "deleteClient",
				"username": username,
			}); cleanupErr != nil {
				log.Printf("Dynamic Security cleanup failed for client %s: %v", username, cleanupErr)
			}
		}
		if roleCreated {
			if cleanupErr := sendDynsecCommand(map[string]interface{}{
				"command":  "deleteRole",
				"rolename": roleName,
			}); cleanupErr != nil {
				log.Printf("Dynamic Security cleanup failed for role %s: %v", roleName, cleanupErr)
			}
		}
	}()

	if err = sendDynsecCommand(map[string]interface{}{
		"command":  "createRole",
		"rolename": roleName,
	}); err != nil {
		return err
	}
	roleCreated = true

	for _, aclType := range []string{"publishClientSend", "publishClientReceive", "subscribePattern"} {
		if err = sendDynsecCommand(map[string]interface{}{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  aclType,
			"topic":    topicPattern,
			"allow":    true,
			"priority": -1,
		}); err != nil {
			return err
		}
	}

	if err = sendDynsecCommand(map[string]interface{}{
		"command":  "createClient",
		"username": username,
		"password": password,
	}); err != nil {
		return err
	}
	clientCreated = true

	if err = sendDynsecCommand(map[string]interface{}{
		"command":  "addClientRole",
		"username": username,
		"rolename": roleName,
		"priority": -1,
	}); err != nil {
		return err
	}

	return nil
}

func CleanupHomeAccess(userID, homeID uint, username string) error {
	roleName := homeRoleName(userID, homeID)
	var errs []string

	if err := sendDynsecCommand(map[string]interface{}{
		"command":  "deleteClient",
		"username": username,
	}); err != nil {
		errs = append(errs, err.Error())
	}

	if err := sendDynsecCommand(map[string]interface{}{
		"command":  "deleteRole",
		"rolename": roleName,
	}); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}
