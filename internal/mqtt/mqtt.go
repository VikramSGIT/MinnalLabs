package mqtt

import (
	"log"
	"strconv"
	"strings"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/state"
)

var Client pahomqtt.Client

func InitMQTT(cfg *config.Config) {
	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(cfg.MQTT.Broker)
	opts.SetClientID(cfg.MQTT.ClientID)

	if cfg.MQTT.Username != "" {
		opts.SetUsername(cfg.MQTT.Username)
		opts.SetPassword(cfg.MQTT.Password)
	}

	opts.SetDefaultPublishHandler(messagePubHandler)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler

	Client = pahomqtt.NewClient(opts)
	if token := Client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Error connecting to MQTT broker: %v", token.Error())
	}
}

// Device presence comes from retained MQTT LWT messages on {user_id}/{home_id}/{device_id}/status.
// Capability state uses {user_id}/{home_id}/{device_id}/{component}/{esphome_key}/state.
var messagePubHandler pahomqtt.MessageHandler = func(client pahomqtt.Client, msg pahomqtt.Message) {
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) == 4 && parts[3] == "status" {
		deviceID, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil {
			return
		}

		payload := string(msg.Payload())
		state.SetDevicePresence(uint(deviceID), payload)
		log.Printf("MQTT: device %d status = %s", deviceID, payload)
		return
	}

	if len(parts) < 6 {
		return
	}

	deviceID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return
	}
	capKey := parts[4]

	state.SetCapState(uint(deviceID), capKey, string(msg.Payload()))
	log.Printf("MQTT: device %d cap %s = %s", deviceID, capKey, msg.Payload())
}

// connectHandler subscribes to retained status topics and capability state topics for all devices.
var connectHandler pahomqtt.OnConnectHandler = func(client pahomqtt.Client) {
	log.Println("Connected to MQTT Broker")
	subscribeDynsecResponses()

	devices := state.GetAllDevices()
	for _, d := range devices {
		Subscribe(models.BuildStatusTopic(d.UserID, d.HomeID, d.ID))

		caps, err := state.GetProductCaps(d.ProductID)
		if err != nil {
			log.Printf("Failed to get caps for product %d: %v", d.ProductID, err)
			continue
		}
		for _, cap := range caps {
			topic := models.BuildTopic(d.UserID, d.HomeID, d.ID, cap.Component, cap.EsphomeKey, "state")
			Subscribe(topic)
		}
	}
}

var connectLostHandler pahomqtt.ConnectionLostHandler = func(client pahomqtt.Client, err error) {
	log.Printf("MQTT Connection lost: %v", err)
}

func Subscribe(topic string) {
	token := Client.Subscribe(topic, 1, nil)
	token.Wait()
	log.Printf("Subscribed to topic: %s", topic)
}

func Unsubscribe(topic string) {
	token := Client.Unsubscribe(topic)
	token.Wait()
	if err := token.Error(); err != nil {
		log.Printf("Failed to unsubscribe from topic %s: %v", topic, err)
		return
	}
	log.Printf("Unsubscribed from topic: %s", topic)
}

func Publish(topic string, payload interface{}) {
	token := Client.Publish(topic, 1, false, payload)
	token.Wait()
	log.Printf("Published to topic: %s", topic)
}

// SubscribeDevice subscribes to all capability state topics for a single device.
func SubscribeDevice(device models.Device) {
	Subscribe(models.BuildStatusTopic(device.UserID, device.HomeID, device.ID))

	caps, err := state.GetProductCaps(device.ProductID)
	if err != nil {
		log.Printf("Failed to get caps for product %d: %v", device.ProductID, err)
		return
	}

	for _, cap := range caps {
		topic := models.BuildTopic(device.UserID, device.HomeID, device.ID, cap.Component, cap.EsphomeKey, "state")
		Subscribe(topic)
	}
}

func UnsubscribeDevice(device models.Device) {
	Unsubscribe(models.BuildStatusTopic(device.UserID, device.HomeID, device.ID))

	caps, err := state.GetProductCaps(device.ProductID)
	if err != nil {
		log.Printf("Failed to get caps for product %d while unsubscribing: %v", device.ProductID, err)
		return
	}

	for _, cap := range caps {
		topic := models.BuildTopic(device.UserID, device.HomeID, device.ID, cap.Component, cap.EsphomeKey, "state")
		Unsubscribe(topic)
	}
}
