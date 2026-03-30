#pragma once

#include <algorithm>
#include <array>
#include <cstdint>
#include <cstring>
#include <string>
#include <vector>

#include <ArduinoJson.h>
#include <esp_log.h>
#include <mbedtls/base64.h>
#include <mbedtls/ctr_drbg.h>
#include <mbedtls/ecdh.h>
#include <mbedtls/entropy.h>
#include <mbedtls/gcm.h>
#include <mbedtls/hkdf.h>
#include <mbedtls/md.h>

namespace secure_ble {

static const char *const TAG = "secure_ble";

static constexpr uint8_t kProvisioningVersion = 1;
static constexpr uint8_t kChunkVersion = 1;
static constexpr size_t kKeySize = 32;
static constexpr size_t kNonceSize = 12;
static constexpr size_t kGcmTagSize = 16;
static constexpr size_t kEnvelopeHeaderSize = 1 + kKeySize + kNonceSize;
static constexpr size_t kChunkHeaderSize = 8;
static constexpr size_t kMaxTransferSize = 2048;
static constexpr size_t kMaxPlaintextSize = 1024;
static constexpr char kHkdfInfo[] = "iot-provisioning-v1";

enum class ChunkStatus {
  kAccepted,
  kComplete,
  kError,
};

struct ProvisioningValues {
  bool valid{false};
  std::string user_id;
  std::string home_id;
  std::string device_id;
  std::string mqtt_host;
  std::string mqtt_port;
  std::string mqtt_username;
  std::string mqtt_password;
  std::string wifi_ssid;
  std::string wifi_password;

  void clear() {
    valid = false;
    user_id.clear();
    home_id.clear();
    device_id.clear();
    mqtt_host.clear();
    mqtt_port.clear();
    mqtt_username.clear();
    mqtt_password.clear();
    wifi_ssid.clear();
    wifi_password.clear();
  }
};

inline std::array<uint8_t, kKeySize> &private_key_bytes() {
  static std::array<uint8_t, kKeySize> value{};
  return value;
}

inline std::array<uint8_t, kKeySize> &public_key_bytes_state() {
  static std::array<uint8_t, kKeySize> value{};
  return value;
}

inline bool &key_material_loaded() {
  static bool value = false;
  return value;
}

inline bool &transfer_active() {
  static bool value = false;
  return value;
}

inline uint8_t &active_transfer_id() {
  static uint8_t value = 0;
  return value;
}

inline uint16_t &expected_chunk_index() {
  static uint16_t value = 0;
  return value;
}

inline uint16_t &expected_total_chunks() {
  static uint16_t value = 0;
  return value;
}

inline uint16_t &expected_total_bytes() {
  static uint16_t value = 0;
  return value;
}

inline std::vector<uint8_t> &transfer_buffer() {
  static std::vector<uint8_t> value;
  return value;
}

inline ProvisioningValues &pending_values() {
  static ProvisioningValues value;
  return value;
}

inline mbedtls_entropy_context &entropy_context() {
  static mbedtls_entropy_context ctx;
  static bool initialized = false;
  if (!initialized) {
    mbedtls_entropy_init(&ctx);
    initialized = true;
  }
  return ctx;
}

inline mbedtls_ctr_drbg_context &ctr_drbg_context() {
  static mbedtls_ctr_drbg_context ctx;
  static bool initialized = false;
  if (!initialized) {
    mbedtls_ctr_drbg_init(&ctx);
    initialized = true;
  }
  return ctx;
}

inline bool ensure_rng_ready(std::string &error) {
  static bool attempted = false;
  static int seed_result = 0;
  if (!attempted) {
    const char *pers = "secure_ble";
    seed_result = mbedtls_ctr_drbg_seed(&ctr_drbg_context(), mbedtls_entropy_func, &entropy_context(),
                                        reinterpret_cast<const unsigned char *>(pers), strlen(pers));
    attempted = true;
  }
  if (seed_result != 0) {
    error = "random generator initialization failed";
    return false;
  }
  return true;
}

inline void reset_transfer_state() {
  transfer_active() = false;
  active_transfer_id() = 0;
  expected_chunk_index() = 0;
  expected_total_chunks() = 0;
  expected_total_bytes() = 0;
  transfer_buffer().clear();
}

inline void clear_runtime_state() {
  reset_transfer_state();
  pending_values().clear();
}

inline void clear_key_cache() {
  key_material_loaded() = false;
  private_key_bytes().fill(0);
  public_key_bytes_state().fill(0);
}

inline uint16_t read_u16_be(const uint8_t *buf) {
  return static_cast<uint16_t>((static_cast<uint16_t>(buf[0]) << 8) | static_cast<uint16_t>(buf[1]));
}

inline int mpi_read_binary_le_compat(mbedtls_mpi *value, const uint8_t *buf, size_t len) {
  std::vector<uint8_t> reversed(buf, buf + len);
  std::reverse(reversed.begin(), reversed.end());
  return mbedtls_mpi_read_binary(value, reversed.data(), reversed.size());
}

inline int mpi_write_binary_le_compat(const mbedtls_mpi *value, uint8_t *buf, size_t len) {
  std::vector<uint8_t> tmp(len);
  int ret = mbedtls_mpi_write_binary(value, tmp.data(), tmp.size());
  if (ret != 0) {
    return ret;
  }
  std::reverse_copy(tmp.begin(), tmp.end(), buf);
  return 0;
}

inline bool base64_encode(const uint8_t *input, size_t input_len, std::string &output, std::string &error) {
  unsigned char encoded[96] = {0};
  size_t encoded_len = 0;
  int ret = mbedtls_base64_encode(encoded, sizeof(encoded), &encoded_len, input, input_len);
  if (ret != 0) {
    error = "base64 encode failed";
    return false;
  }
  output.assign(reinterpret_cast<char *>(encoded), encoded_len);
  return true;
}

inline bool base64_decode_32(const std::string &input, std::array<uint8_t, kKeySize> &output, std::string &error) {
  size_t output_len = 0;
  std::array<uint8_t, 64> decoded{};
  int ret = mbedtls_base64_decode(decoded.data(), decoded.size(), &output_len,
                                  reinterpret_cast<const unsigned char *>(input.data()), input.size());
  if (ret != 0 || output_len != kKeySize) {
    error = "invalid base64 key";
    return false;
  }
  std::copy(decoded.begin(), decoded.begin() + kKeySize, output.begin());
  return true;
}

inline bool generate_keypair(std::array<uint8_t, kKeySize> &private_key,
                             std::array<uint8_t, kKeySize> &public_key,
                             std::string &error) {
  if (!ensure_rng_ready(error)) {
    return false;
  }

  mbedtls_ecp_group grp;
  mbedtls_ecp_point public_point;
  mbedtls_mpi private_mpi;
  mbedtls_ecp_group_init(&grp);
  mbedtls_ecp_point_init(&public_point);
  mbedtls_mpi_init(&private_mpi);

  int ret = mbedtls_ecp_group_load(&grp, MBEDTLS_ECP_DP_CURVE25519);
  if (ret == 0) {
    ret = mbedtls_ecdh_gen_public(&grp, &private_mpi, &public_point,
                                  mbedtls_ctr_drbg_random, &ctr_drbg_context());
  }
  if (ret == 0) {
    ret = mpi_write_binary_le_compat(&private_mpi, private_key.data(), private_key.size());
  }
  if (ret == 0) {
    ret = mpi_write_binary_le_compat(&public_point.MBEDTLS_PRIVATE(X),
                                     public_key.data(), public_key.size());
  }

  mbedtls_mpi_free(&private_mpi);
  mbedtls_ecp_point_free(&public_point);
  mbedtls_ecp_group_free(&grp);

  if (ret != 0) {
    error = "failed to generate X25519 keypair";
    return false;
  }
  return true;
}

inline bool ensure_key_material(std::string &private_key_b64,
                                std::string &public_key_b64,
                                std::string &error) {
  clear_runtime_state();

  std::array<uint8_t, kKeySize> restored_private{};
  std::array<uint8_t, kKeySize> restored_public{};
  std::string decode_error;

  if (!private_key_b64.empty() && !public_key_b64.empty() &&
      base64_decode_32(private_key_b64, restored_private, decode_error) &&
      base64_decode_32(public_key_b64, restored_public, decode_error)) {
    private_key_bytes() = restored_private;
    public_key_bytes_state() = restored_public;
    key_material_loaded() = true;
    return true;
  }

  std::array<uint8_t, kKeySize> new_private{};
  std::array<uint8_t, kKeySize> new_public{};
  if (!generate_keypair(new_private, new_public, error)) {
    return false;
  }

  std::string encoded_private;
  if (!base64_encode(new_private.data(), new_private.size(), encoded_private, error)) {
    return false;
  }
  std::string encoded_public;
  if (!base64_encode(new_public.data(), new_public.size(), encoded_public, error)) {
    return false;
  }

  private_key_b64 = encoded_private;
  public_key_b64 = encoded_public;
  private_key_bytes() = new_private;
  public_key_bytes_state() = new_public;
  key_material_loaded() = true;
  return true;
}

inline std::vector<uint8_t> public_key_bytes() {
  return std::vector<uint8_t>(public_key_bytes_state().begin(), public_key_bytes_state().end());
}

inline bool derive_shared_secret(const uint8_t *peer_public_key,
                                 uint8_t *shared_secret,
                                 std::string &error) {
  if (!ensure_rng_ready(error)) {
    return false;
  }

  mbedtls_ecp_group grp;
  mbedtls_ecp_point peer_point;
  mbedtls_mpi private_mpi;
  mbedtls_mpi shared;
  mbedtls_ecp_group_init(&grp);
  mbedtls_ecp_point_init(&peer_point);
  mbedtls_mpi_init(&private_mpi);
  mbedtls_mpi_init(&shared);

  int ret = mbedtls_ecp_group_load(&grp, MBEDTLS_ECP_DP_CURVE25519);
  if (ret == 0) {
    ret = mpi_read_binary_le_compat(&private_mpi, private_key_bytes().data(), private_key_bytes().size());
  }
  if (ret == 0) {
    ret = mpi_read_binary_le_compat(&peer_point.MBEDTLS_PRIVATE(X), peer_public_key, kKeySize);
  }
  if (ret == 0) {
    ret = mbedtls_mpi_lset(&peer_point.MBEDTLS_PRIVATE(Z), 1);
  }
  if (ret == 0) {
    ret = mbedtls_ecdh_compute_shared(&grp, &shared, &peer_point, &private_mpi,
                                      mbedtls_ctr_drbg_random, &ctr_drbg_context());
  }
  if (ret == 0) {
    ret = mpi_write_binary_le_compat(&shared, shared_secret, kKeySize);
  }

  mbedtls_mpi_free(&private_mpi);
  mbedtls_ecp_point_free(&peer_point);
  mbedtls_ecp_group_free(&grp);
  mbedtls_mpi_free(&shared);

  if (ret != 0) {
    error = "failed to derive shared secret";
    return false;
  }
  return true;
}

inline bool decrypt_envelope(const std::vector<uint8_t> &envelope,
                             std::vector<uint8_t> &plaintext,
                             std::string &error) {
  if (!key_material_loaded()) {
    error = "device key material unavailable";
    return false;
  }
  if (envelope.size() < kEnvelopeHeaderSize + kGcmTagSize) {
    error = "encrypted envelope too short";
    return false;
  }
  if (envelope[0] != kProvisioningVersion) {
    error = "unsupported provisioning envelope version";
    return false;
  }

  const uint8_t *server_public_key = envelope.data() + 1;
  const uint8_t *nonce = envelope.data() + 1 + kKeySize;
  const uint8_t *ciphertext = envelope.data() + kEnvelopeHeaderSize;
  const size_t ciphertext_and_tag_len = envelope.size() - kEnvelopeHeaderSize;
  if (ciphertext_and_tag_len <= kGcmTagSize) {
    error = "encrypted envelope missing tag";
    return false;
  }

  const size_t ciphertext_len = ciphertext_and_tag_len - kGcmTagSize;
  const uint8_t *tag = ciphertext + ciphertext_len;

  uint8_t shared_secret[kKeySize] = {0};
  if (!derive_shared_secret(server_public_key, shared_secret, error)) {
    return false;
  }

  uint8_t aes_key[kKeySize] = {0};
  const mbedtls_md_info_t *sha256_info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
  if (sha256_info == nullptr) {
    error = "sha256 unavailable";
    return false;
  }
  int ret = mbedtls_hkdf(sha256_info, nullptr, 0, shared_secret, sizeof(shared_secret),
                         reinterpret_cast<const unsigned char *>(kHkdfInfo), strlen(kHkdfInfo),
                         aes_key, sizeof(aes_key));
  if (ret != 0) {
    error = "hkdf failed";
    return false;
  }

  plaintext.assign(ciphertext_len, 0);
  mbedtls_gcm_context gcm;
  mbedtls_gcm_init(&gcm);
  ret = mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, aes_key, sizeof(aes_key) * 8);
  if (ret == 0) {
    ret = mbedtls_gcm_auth_decrypt(&gcm, ciphertext_len, nonce, kNonceSize, nullptr, 0,
                                   tag, kGcmTagSize, ciphertext, plaintext.data());
  }
  mbedtls_gcm_free(&gcm);

  if (ret != 0) {
    error = "aes-gcm decrypt failed";
    plaintext.clear();
    return false;
  }
  return true;
}

inline bool parse_plaintext_payload(const std::vector<uint8_t> &plaintext,
                                    std::string &error) {
  if (plaintext.empty() || plaintext.size() > kMaxPlaintextSize) {
    error = "invalid plaintext payload size";
    return false;
  }

  StaticJsonDocument<1024> doc;
  DeserializationError parse_error = deserializeJson(doc, plaintext.data(), plaintext.size());
  if (parse_error) {
    error = "provisioning JSON parse failed";
    return false;
  }

  auto require_string = [&](const char *key, std::string *out) -> bool {
    JsonVariantConst value = doc[key];
    if (value.isNull() || !value.is<const char *>()) {
      error = std::string("missing provisioning field: ") + key;
      return false;
    }
    *out = std::string(value.as<const char *>());
    return true;
  };

  ProvisioningValues parsed;
  if (!require_string("user_id", &parsed.user_id) ||
      !require_string("home_id", &parsed.home_id) ||
      !require_string("device_id", &parsed.device_id) ||
      !require_string("mqtt_host", &parsed.mqtt_host) ||
      !require_string("mqtt_port", &parsed.mqtt_port) ||
      !require_string("mqtt_username", &parsed.mqtt_username) ||
      !require_string("mqtt_password", &parsed.mqtt_password) ||
      !require_string("wifi_ssid", &parsed.wifi_ssid) ||
      !require_string("wifi_password", &parsed.wifi_password)) {
    return false;
  }

  parsed.valid = true;
  pending_values() = parsed;
  return true;
}

inline ChunkStatus ingest_chunk(const std::vector<uint8_t> &packet, std::string &error) {
  if (!key_material_loaded()) {
    error = "secure key material not initialized";
    return ChunkStatus::kError;
  }
  if (packet.size() < kChunkHeaderSize) {
    error = "chunk too short";
    return ChunkStatus::kError;
  }

  const uint8_t version = packet[0];
  const uint8_t transfer_id = packet[1];
  const uint16_t chunk_index = read_u16_be(packet.data() + 2);
  const uint16_t total_chunks = read_u16_be(packet.data() + 4);
  const uint16_t total_bytes = read_u16_be(packet.data() + 6);
  const size_t payload_len = packet.size() - kChunkHeaderSize;

  if (version != kChunkVersion) {
    error = "unsupported chunk protocol version";
    reset_transfer_state();
    return ChunkStatus::kError;
  }
  if (total_chunks == 0 || total_bytes == 0) {
    error = "invalid chunk metadata";
    reset_transfer_state();
    return ChunkStatus::kError;
  }
  if (total_bytes > kMaxTransferSize) {
    error = "encrypted bundle exceeds supported size";
    reset_transfer_state();
    return ChunkStatus::kError;
  }
  if (chunk_index >= total_chunks) {
    error = "chunk index out of range";
    reset_transfer_state();
    return ChunkStatus::kError;
  }

  if (!transfer_active() || transfer_id != active_transfer_id()) {
    if (chunk_index != 0) {
      error = "received non-initial chunk without active transfer";
      reset_transfer_state();
      return ChunkStatus::kError;
    }
    reset_transfer_state();
    pending_values().clear();
    transfer_active() = true;
    active_transfer_id() = transfer_id;
    expected_chunk_index() = 0;
    expected_total_chunks() = total_chunks;
    expected_total_bytes() = total_bytes;
    transfer_buffer().reserve(total_bytes);
  }

  if (transfer_id != active_transfer_id() ||
      total_chunks != expected_total_chunks() ||
      total_bytes != expected_total_bytes()) {
    error = "chunk metadata mismatch";
    reset_transfer_state();
    return ChunkStatus::kError;
  }
  if (chunk_index != expected_chunk_index()) {
    error = "chunk order mismatch";
    reset_transfer_state();
    return ChunkStatus::kError;
  }
  if (transfer_buffer().size() + payload_len > expected_total_bytes()) {
    error = "transfer buffer overflow";
    reset_transfer_state();
    return ChunkStatus::kError;
  }

  transfer_buffer().insert(transfer_buffer().end(),
                           packet.begin() + static_cast<std::ptrdiff_t>(kChunkHeaderSize),
                           packet.end());
  expected_chunk_index()++;

  if (expected_chunk_index() < expected_total_chunks()) {
    return ChunkStatus::kAccepted;
  }

  if (transfer_buffer().size() != expected_total_bytes()) {
    error = "incomplete encrypted bundle";
    reset_transfer_state();
    return ChunkStatus::kError;
  }

  std::vector<uint8_t> plaintext;
  if (!decrypt_envelope(transfer_buffer(), plaintext, error)) {
    reset_transfer_state();
    return ChunkStatus::kError;
  }
  if (!parse_plaintext_payload(plaintext, error)) {
    reset_transfer_state();
    return ChunkStatus::kError;
  }

  reset_transfer_state();
  return ChunkStatus::kComplete;
}

inline bool has_pending_provisioning() {
  return pending_values().valid;
}

inline bool apply_pending_provisioning(std::string &user_id,
                                       std::string &home_id,
                                       std::string &device_id,
                                       std::string &mqtt_host,
                                       std::string &mqtt_port,
                                       std::string &mqtt_username,
                                       std::string &mqtt_password,
                                       std::string &wifi_ssid,
                                       std::string &wifi_password,
                                       std::string &error) {
  if (!pending_values().valid) {
    error = "no validated provisioning bundle available";
    return false;
  }

  user_id = pending_values().user_id;
  home_id = pending_values().home_id;
  device_id = pending_values().device_id;
  mqtt_host = pending_values().mqtt_host;
  mqtt_port = pending_values().mqtt_port;
  mqtt_username = pending_values().mqtt_username;
  mqtt_password = pending_values().mqtt_password;
  wifi_ssid = pending_values().wifi_ssid;
  wifi_password = pending_values().wifi_password;

  pending_values().clear();
  return true;
}

}  // namespace secure_ble
