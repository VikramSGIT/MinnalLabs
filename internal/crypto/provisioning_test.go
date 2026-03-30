package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"golang.org/x/crypto/hkdf"
)

func TestParseDevicePublicKey_Valid(t *testing.T) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	b64 := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())
	pub, err := ParseDevicePublicKey(b64)
	if err != nil {
		t.Fatalf("ParseDevicePublicKey: %v", err)
	}
	if !pub.Equal(priv.PublicKey()) {
		t.Fatal("parsed key does not match original")
	}
}

func TestParseDevicePublicKey_InvalidBase64(t *testing.T) {
	_, err := ParseDevicePublicKey("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestParseDevicePublicKey_WrongLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too-short"))
	_, err := ParseDevicePublicKey(short)
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
	if !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("error should mention 32 bytes, got: %v", err)
	}
}

func TestEncryptProvisioningBundle_RoundTrip(t *testing.T) {
	devicePriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate device key: %v", err)
	}

	payload := ProvisioningPayload{
		DeviceID:                "42",
		UserID:                  "7",
		HomeID:                  "3",
		MQTTHost:                "mqtt.example.com",
		MQTTPort:                "1883",
		MQTTUsername:            "home_7_3_abc123",
		MQTTPassword:            "supersecretpassword",
		MQTTConnectDelaySeconds: 15,
		WiFiSSID:                "TestNetwork",
		WiFiPassword:            "wifipass123",
	}

	b64Pub := base64.StdEncoding.EncodeToString(devicePriv.PublicKey().Bytes())
	devicePubKey, err := ParseDevicePublicKey(b64Pub)
	if err != nil {
		t.Fatalf("parse pub key: %v", err)
	}

	bundleB64, err := EncryptProvisioningBundle(devicePubKey, payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	envelope, err := base64.StdEncoding.DecodeString(bundleB64)
	if err != nil {
		t.Fatalf("decode bundle base64: %v", err)
	}

	if len(envelope) < 1+32+12+16 {
		t.Fatalf("envelope too short: %d bytes", len(envelope))
	}

	version := envelope[0]
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}

	serverEPKBytes := envelope[1:33]
	nonce := envelope[33:45]
	ciphertext := envelope[45:]

	serverEPK, err := ecdh.X25519().NewPublicKey(serverEPKBytes)
	if err != nil {
		t.Fatalf("parse server epk: %v", err)
	}

	shared, err := devicePriv.ECDH(serverEPK)
	if err != nil {
		t.Fatalf("device ecdh: %v", err)
	}

	hkdfReader := hkdf.New(sha256.New, shared, nil, []byte("iot-provisioning-v1"))
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		t.Fatalf("hkdf: %v", err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	var recovered ProvisioningPayload
	if err := json.Unmarshal(plaintext, &recovered); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if recovered != payload {
		t.Fatalf("payload mismatch:\n  got:  %+v\n  want: %+v", recovered, payload)
	}
}

func TestEncryptProvisioningBundle_UniquePerCall(t *testing.T) {
	devicePriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pub, _ := ParseDevicePublicKey(base64.StdEncoding.EncodeToString(devicePriv.PublicKey().Bytes()))
	payload := ProvisioningPayload{DeviceID: "1", UserID: "1", HomeID: "1"}

	bundle1, err := EncryptProvisioningBundle(pub, payload)
	if err != nil {
		t.Fatalf("encrypt 1: %v", err)
	}
	bundle2, err := EncryptProvisioningBundle(pub, payload)
	if err != nil {
		t.Fatalf("encrypt 2: %v", err)
	}

	if bundle1 == bundle2 {
		t.Fatal("two encryptions of the same payload must produce different ciphertexts (ephemeral key + nonce)")
	}
}

func TestEncryptProvisioningBundle_NoPaintextSecretsInOutput(t *testing.T) {
	devicePriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pub, _ := ParseDevicePublicKey(base64.StdEncoding.EncodeToString(devicePriv.PublicKey().Bytes()))

	payload := ProvisioningPayload{
		MQTTPassword: "hunter2_mqtt_secret",
		WiFiPassword: "hunter2_wifi_secret",
	}

	bundleB64, err := EncryptProvisioningBundle(pub, payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if strings.Contains(bundleB64, "hunter2_mqtt_secret") {
		t.Fatal("MQTT password appears in plaintext in the bundle")
	}
	if strings.Contains(bundleB64, "hunter2_wifi_secret") {
		t.Fatal("Wi-Fi password appears in plaintext in the bundle")
	}
}
