package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const provisioningVersion byte = 1

// ProvisioningPayload is the plaintext that gets encrypted inside the bundle.
type ProvisioningPayload struct {
	DeviceID     string `json:"device_id"`
	UserID       string `json:"user_id"`
	HomeID       string `json:"home_id"`
	MQTTHost     string `json:"mqtt_host"`
	MQTTPort     string `json:"mqtt_port"`
	MQTTUsername string `json:"mqtt_username"`
	MQTTPassword string `json:"mqtt_password"`
	WiFiSSID     string `json:"wifi_ssid"`
	WiFiPassword string `json:"wifi_password"`
}

// ParseDevicePublicKey decodes a base64-encoded 32-byte X25519 public key.
func ParseDevicePublicKey(b64 string) (*ecdh.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(raw))
	}
	return ecdh.X25519().NewPublicKey(raw)
}

// EncryptProvisioningBundle encrypts a provisioning payload for the device.
//
// Wire format (binary, then base64-encoded for JSON transport):
//
//	[1B version] [32B server ephemeral pubkey] [12B nonce] [ciphertext + 16B GCM tag]
//
// Key derivation: ECDH(server_ephemeral, device_public) -> HKDF-SHA256 -> AES-256-GCM key.
func EncryptProvisioningBundle(devicePubKey *ecdh.PublicKey, payload ProvisioningPayload) (string, error) {
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ephemeral key: %w", err)
	}

	shared, err := ephemeral.ECDH(devicePubKey)
	if err != nil {
		return "", fmt.Errorf("ecdh: %w", err)
	}

	hkdfReader := hkdf.New(sha256.New, shared, nil, []byte("iot-provisioning-v1"))
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return "", fmt.Errorf("hkdf: %w", err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}

	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	epkBytes := ephemeral.PublicKey().Bytes()
	envelope := make([]byte, 0, 1+len(epkBytes)+len(nonce)+len(ciphertext))
	envelope = append(envelope, provisioningVersion)
	envelope = append(envelope, epkBytes...)
	envelope = append(envelope, nonce...)
	envelope = append(envelope, ciphertext...)

	return base64.StdEncoding.EncodeToString(envelope), nil
}
