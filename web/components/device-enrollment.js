import { ApiError, postJSON, requestJSON } from "../lib/api.js";
import { escapeHtml } from "../lib/html.js";
import { formatTimestamp } from "../lib/format.js";

const SERVICE_UUID = "12345678-1234-1234-1234-000000000000";
const UUIDS = {
  productType: "12345678-1234-1234-1234-000000000001",
  productId: "12345678-1234-1234-1234-00000000000c",
  devicePublicKey: "12345678-1234-1234-1234-00000000000d",
  devicePublicKeyRequest: "12345678-1234-1234-1234-00000000000f",
  provisioningBlob: "12345678-1234-1234-1234-00000000000e",
  restart: "12345678-1234-1234-1234-00000000000b",
};

const MQTT_WAIT_TIMEOUT_MS = 60_000;
const MQTT_WAIT_INTERVAL_MS = 2_000;
const CHUNK_VERSION = 1;
const CHUNK_HEADER_SIZE = 8;
const CHUNK_PAYLOAD_SIZE = 120;
const PUBLIC_KEY_REQUEST_SIZE = 4;
const DEVICE_PUBLIC_KEY_BYTES = 32;

function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function toUint8Array(bytes) {
  if (bytes instanceof Uint8Array) {
    return bytes;
  }
  if (bytes instanceof DataView) {
    return new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  }
  if (ArrayBuffer.isView(bytes)) {
    return new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  }
  if (bytes instanceof ArrayBuffer) {
    return new Uint8Array(bytes);
  }
  throw new Error("Expected bytes from Bluetooth.");
}

function bytesToBase64(bytes) {
  const normalized = toUint8Array(bytes);
  let binary = "";
  for (let i = 0; i < normalized.byteLength; i++) {
    binary += String.fromCharCode(normalized[i]);
  }
  return window.btoa(binary);
}

function base64ToBytes(b64) {
  const binary = window.atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function writeUInt16BE(target, offset, value) {
  target[offset] = (value >> 8) & 0xff;
  target[offset + 1] = value & 0xff;
}

function readUInt16BE(source, offset) {
  return (source[offset] << 8) | source[offset + 1];
}

function parseChunkPacket(packetBytes, label) {
  const packet = toUint8Array(packetBytes);
  if (packet.length < CHUNK_HEADER_SIZE) {
    throw new Error(`${label} packet is too short.`);
  }

  const version = packet[0];
  const transferId = packet[1];
  const chunkIndex = readUInt16BE(packet, 2);
  const totalChunks = readUInt16BE(packet, 4);
  const totalBytes = readUInt16BE(packet, 6);
  const payload = packet.subarray(CHUNK_HEADER_SIZE);

  if (version !== CHUNK_VERSION) {
    throw new Error(`${label} packet has unsupported version ${version}.`);
  }
  if (totalChunks === 0) {
    throw new Error(`${label} packet reported zero chunks.`);
  }
  if (chunkIndex >= totalChunks) {
    throw new Error(`${label} packet has out-of-range chunk index ${chunkIndex}.`);
  }
  if (totalBytes === 0) {
    throw new Error(`${label} packet reported zero bytes.`);
  }

  return { transferId, chunkIndex, totalChunks, totalBytes, payload };
}

function buildPublicKeyChunkRequest(transferId, chunkIndex) {
  const request = new Uint8Array(PUBLIC_KEY_REQUEST_SIZE);
  request[0] = CHUNK_VERSION;
  request[1] = transferId;
  writeUInt16BE(request, 2, chunkIndex);
  return request;
}

function buildProvisioningChunks(bundleBytes) {
  if (!(bundleBytes instanceof Uint8Array) || bundleBytes.length === 0) {
    throw new Error("Encrypted provisioning bundle is empty.");
  }
  if (bundleBytes.length > 0xffff) {
    throw new Error("Encrypted provisioning bundle is too large for BLE transport.");
  }

  const totalChunks = Math.ceil(bundleBytes.length / CHUNK_PAYLOAD_SIZE);
  if (totalChunks > 0xffff) {
    throw new Error("Encrypted provisioning bundle requires too many BLE chunks.");
  }

  const transferId = window.crypto?.getRandomValues
    ? window.crypto.getRandomValues(new Uint8Array(1))[0]
    : Math.floor(Math.random() * 256);

  const packets = [];
  for (let chunkIndex = 0; chunkIndex < totalChunks; chunkIndex += 1) {
    const start = chunkIndex * CHUNK_PAYLOAD_SIZE;
    const end = Math.min(start + CHUNK_PAYLOAD_SIZE, bundleBytes.length);
    const payload = bundleBytes.subarray(start, end);
    const packet = new Uint8Array(CHUNK_HEADER_SIZE + payload.length);

    packet[0] = CHUNK_VERSION;
    packet[1] = transferId;
    writeUInt16BE(packet, 2, chunkIndex);
    writeUInt16BE(packet, 4, totalChunks);
    writeUInt16BE(packet, 6, bundleBytes.length);
    packet.set(payload, CHUNK_HEADER_SIZE);

    packets.push(packet);
  }

  return packets;
}

async function writeWithResponse(characteristic, bytes) {
  if (typeof characteristic.writeValueWithResponse === "function") {
    await characteristic.writeValueWithResponse(bytes);
    return;
  }
  await characteristic.writeValue(bytes);
}

async function readChunkedDevicePublicKey(mainService) {
  let chunkCharacteristic;
  let requestCharacteristic;
  try {
    [chunkCharacteristic, requestCharacteristic] = await Promise.all([
      mainService.getCharacteristic(UUIDS.devicePublicKey),
      mainService.getCharacteristic(UUIDS.devicePublicKeyRequest),
    ]);
  } catch (_) {
    throw new Error("Device firmware does not support chunked secure enrollment (missing public key transfer characteristic). Update the firmware first.");
  }

  const firstChunk = parseChunkPacket(await chunkCharacteristic.readValue(), "Device public key");
  if (firstChunk.chunkIndex !== 0) {
    throw new Error("Device public key transfer did not start at chunk 0.");
  }
  if (firstChunk.totalBytes !== DEVICE_PUBLIC_KEY_BYTES) {
    throw new Error(`Device public key must be ${DEVICE_PUBLIC_KEY_BYTES} bytes, got ${firstChunk.totalBytes}.`);
  }

  const assembled = new Uint8Array(firstChunk.totalBytes);
  let offset = 0;
  assembled.set(firstChunk.payload, offset);
  offset += firstChunk.payload.length;

  for (let chunkIndex = 1; chunkIndex < firstChunk.totalChunks; chunkIndex += 1) {
    const request = buildPublicKeyChunkRequest(firstChunk.transferId, chunkIndex);
    await writeWithResponse(requestCharacteristic, request);
    const packet = parseChunkPacket(await chunkCharacteristic.readValue(), "Device public key");

    if (packet.transferId !== firstChunk.transferId) {
      throw new Error("Device public key transfer ID changed mid-transfer.");
    }
    if (packet.chunkIndex !== chunkIndex) {
      throw new Error(`Expected device public key chunk ${chunkIndex}, got ${packet.chunkIndex}.`);
    }
    if (packet.totalChunks !== firstChunk.totalChunks || packet.totalBytes !== firstChunk.totalBytes) {
      throw new Error("Device public key transfer metadata changed mid-transfer.");
    }

    assembled.set(packet.payload, offset);
    offset += packet.payload.length;
  }

  if (offset !== firstChunk.totalBytes) {
    throw new Error(`Device public key transfer incomplete: expected ${firstChunk.totalBytes} bytes, got ${offset}.`);
  }

  return { bytes: assembled, totalChunks: firstChunk.totalChunks };
}

function formatReconnectStatus(status) {
  if (!status) {
    return "Waiting for reconnect";
  }

  if (status.mqtt_connected) {
    return "Connected after reboot";
  }

  if (status.mqtt_status === "offline") {
    return "Seen offline";
  }

  if (status.mqtt_status === "unknown") {
    return "Waiting for first MQTT status";
  }

  return "Waiting for reconnect";
}

class DeviceEnrollment extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.apiBaseUrl = "";
    this.user = null;
    this.home = null;
    this.bluetoothDevice = null;
    this.gattServer = null;
    this.mainService = null;
    this.productName = "";
    this.productId = "";
    this.devicePublicKeyB64 = "";
    this.isWorking = false;
    this.currentAction = "";
    this.onDisconnected = this.handleDisconnected.bind(this);
  }

  connectedCallback() {
    this.render();
    this.cacheDom();
    this.bindEvents();
    this.syncUi();
    this.log("Connect to a device whose advertised name starts with ml-.");
  }

  disconnectedCallback() {
    if (this.bluetoothDevice) {
      this.bluetoothDevice.removeEventListener("gattserverdisconnected", this.onDisconnected);
    }
  }

  render() {
    const userLabel = this.user ? `${this.user.username} (ID ${this.user.user_id})` : "Missing";
    const homeLabel = this.home ? `${this.home.name} (ID ${this.home.home_id})` : "Missing";

    this.shadowRoot.innerHTML = `
      <style>
        :host {
          display: block;
        }

        *, *::before, *::after {
          box-sizing: border-box;
        }

        .panel {
          display: grid;
          gap: 20px;
          padding: 28px;
          border-radius: 24px;
          border: 1px solid #dbeafe;
          background: #ffffff;
          box-shadow: 0 20px 44px rgba(15, 23, 42, 0.08);
        }

        h2, h3 {
          margin: 0;
        }

        p {
          margin: 0;
          color: #475569;
          line-height: 1.6;
        }

        .meta {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
          gap: 16px;
        }

        .meta-card,
        .section {
          border-radius: 18px;
          border: 1px solid #e2e8f0;
          background: #f8fafc;
          padding: 18px;
        }

        .status-bar {
          display: flex;
          align-items: center;
          gap: 14px;
          flex-wrap: wrap;
        }

        .status-dot {
          width: 14px;
          height: 14px;
          border-radius: 999px;
          background: #ef4444;
          box-shadow: 0 0 0 4px rgba(239, 68, 68, 0.15);
        }

        .status-dot.connected {
          background: #22c55e;
          box-shadow: 0 0 0 4px rgba(34, 197, 94, 0.18);
        }

        .status-text {
          font-weight: 700;
          color: #0f172a;
        }

        .button-row {
          display: flex;
          gap: 12px;
          flex-wrap: wrap;
          margin-left: auto;
        }

        button {
          border: 0;
          border-radius: 14px;
          padding: 0.85rem 1.1rem;
          font: inherit;
          font-weight: 700;
          cursor: pointer;
          color: #0f172a;
          background: #e2e8f0;
        }

        button.primary {
          color: #ffffff;
          background: #2563eb;
        }

        button.danger {
          color: #ffffff;
          background: #dc2626;
        }

        button:disabled {
          opacity: 0.65;
          cursor: not-allowed;
        }

        .grid {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
          gap: 16px;
        }

        label {
          display: grid;
          gap: 8px;
          font-weight: 600;
          color: #0f172a;
        }

        input {
          width: 100%;
          padding: 0.9rem 1rem;
          border-radius: 14px;
          border: 1px solid #cbd5e1;
          font: inherit;
          color: #0f172a;
          background: #ffffff;
        }

        input[readonly] {
          background: #f1f5f9;
          color: #475569;
        }

        .error {
          min-height: 1.2rem;
          color: #b91c1c;
          font-size: 0.95rem;
        }

        .note {
          font-size: 0.95rem;
        }

        .summary {
          display: none;
          grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
          gap: 14px;
        }

        .summary.visible {
          display: grid;
        }

        .summary-item {
          border-radius: 14px;
          border: 1px solid #bbf7d0;
          background: #f0fdf4;
          padding: 14px;
        }

        .summary-item strong {
          display: block;
          color: #166534;
          font-size: 0.85rem;
          text-transform: uppercase;
          letter-spacing: 0.08em;
          margin-bottom: 6px;
        }

        .summary-item span {
          color: #14532d;
          word-break: break-word;
        }

        .log-toolbar {
          display: flex;
          justify-content: space-between;
          gap: 12px;
          align-items: center;
          flex-wrap: wrap;
        }

        .log {
          min-height: 220px;
          max-height: 300px;
          overflow: auto;
          padding: 16px;
          border-radius: 18px;
          background: #0f172a;
          color: #e2e8f0;
          font-family: "Fira Code", "SFMono-Regular", monospace;
          font-size: 0.9rem;
        }

        .log-entry {
          display: flex;
          gap: 10px;
          margin-bottom: 10px;
          line-height: 1.5;
        }

        .log-time {
          color: #94a3b8;
          white-space: nowrap;
        }

        .log-entry.info .log-message {
          color: #bfdbfe;
        }

        .log-entry.success .log-message {
          color: #86efac;
        }

        .log-entry.error .log-message {
          color: #fda4af;
        }
      </style>
      <section class="panel">
        <div>
          <h2>Enroll Device</h2>
          <p>Read the device identity and public key over Bluetooth, create the backend device record, then write an encrypted provisioning bundle to the device over secure BLE chunks.</p>
        </div>

        <div class="meta">
          <div class="meta-card">
            <h3>User</h3>
            <p>${escapeHtml(userLabel)}</p>
          </div>
          <div class="meta-card">
            <h3>Home</h3>
            <p>${escapeHtml(homeLabel)}</p>
          </div>
        </div>

        <div class="section status-bar">
          <span id="statusDot" class="status-dot"></span>
          <span id="statusText" class="status-text">Disconnected</span>
          <div class="button-row">
            <button id="connectBtn" class="primary" type="button">Connect Device</button>
            <button id="disconnectBtn" class="danger" type="button" disabled>Disconnect</button>
            <button id="readBtn" type="button" disabled>Read Product Info</button>
          </div>
        </div>

        <div class="section">
          <div class="grid">
            <label>
              Product Name
              <input id="productNameInput" type="text" readonly placeholder="Read from Bluetooth">
            </label>
            <label>
              Product ID
              <input id="productIdInput" type="text" readonly placeholder="Read from Bluetooth">
            </label>
            <label>
              Device Name
              <input id="deviceNameInput" type="text" placeholder="Kitchen Sensor" required>
            </label>
          </div>
          <p class="note">This flow reads the device public key, sends it to the server, and writes the encrypted provisioning bundle to the device in sequenced BLE chunks.</p>
          <div id="error" class="error" role="alert"></div>
          <div style="margin-top: 18px;">
            <button id="enrollBtn" class="primary" type="button" disabled>Enroll And Provision</button>
          </div>
        </div>

        <div id="summary" class="summary"></div>

        <div class="section">
          <div class="log-toolbar">
            <h3>Provisioning Log</h3>
            <button id="clearLogBtn" type="button">Clear Log</button>
          </div>
          <div id="log" class="log" aria-live="polite"></div>
        </div>
      </section>
    `;
  }

  cacheDom() {
    this.statusDot = this.shadowRoot.getElementById("statusDot");
    this.statusText = this.shadowRoot.getElementById("statusText");
    this.connectBtn = this.shadowRoot.getElementById("connectBtn");
    this.disconnectBtn = this.shadowRoot.getElementById("disconnectBtn");
    this.readBtn = this.shadowRoot.getElementById("readBtn");
    this.enrollBtn = this.shadowRoot.getElementById("enrollBtn");
    this.clearLogBtn = this.shadowRoot.getElementById("clearLogBtn");
    this.productNameInput = this.shadowRoot.getElementById("productNameInput");
    this.productIdInput = this.shadowRoot.getElementById("productIdInput");
    this.deviceNameInput = this.shadowRoot.getElementById("deviceNameInput");
    this.errorEl = this.shadowRoot.getElementById("error");
    this.logEl = this.shadowRoot.getElementById("log");
    this.summaryEl = this.shadowRoot.getElementById("summary");
  }

  bindEvents() {
    this.connectBtn.addEventListener("click", () => this.connectDevice());
    this.disconnectBtn.addEventListener("click", () => this.disconnectDevice());
    this.readBtn.addEventListener("click", () => this.readProductInfo());
    this.enrollBtn.addEventListener("click", () => this.enrollAndProvision());
    this.clearLogBtn.addEventListener("click", () => {
      this.logEl.innerHTML = "";
    });
  }

  syncUi() {
    const isConnected = Boolean(this.gattServer && this.gattServer.connected && this.mainService);
    this.statusDot.classList.toggle("connected", isConnected);
    this.statusText.textContent = isConnected ? "Connected" : "Disconnected";
    this.connectBtn.disabled = this.isWorking || isConnected;
    this.disconnectBtn.disabled = !isConnected;
    this.readBtn.disabled = this.isWorking || !isConnected;
    this.enrollBtn.disabled = this.isWorking || !isConnected;
    this.productNameInput.value = this.productName;
    this.productIdInput.value = this.productId;
    this.connectBtn.textContent = this.currentAction === "connecting" ? "Connecting..." : "Connect Device";
    this.enrollBtn.textContent = this.currentAction === "provisioning" ? "Provisioning..." : "Enroll And Provision";
  }

  async connectDevice() {
    if (this.isWorking) {
      return;
    }

    if (!navigator.bluetooth) {
      this.setError("Web Bluetooth is not supported in this browser.");
      return;
    }

    this.setError("");
    this.isWorking = true;
    this.currentAction = "connecting";
    this.syncUi();

    try {
      this.log("Requesting Bluetooth device...", "info");
      this.bluetoothDevice = await navigator.bluetooth.requestDevice({
        filters: [{ namePrefix: "ml-" }],
        optionalServices: [SERVICE_UUID],
      });

      this.bluetoothDevice.removeEventListener("gattserverdisconnected", this.onDisconnected);
      this.bluetoothDevice.addEventListener("gattserverdisconnected", this.onDisconnected);

      this.log(`Connecting to ${this.bluetoothDevice.name || "device"}...`, "info");
      this.gattServer = await this.bluetoothDevice.gatt.connect();
      this.mainService = await this.gattServer.getPrimaryService(SERVICE_UUID);
      this.log("Connected to provisioning service.", "success");

      await this.readProductInfo();
    } catch (error) {
      this.setError(error.message);
      this.log(`Bluetooth connection failed: ${error.message}`, "error");
      this.mainService = null;
      this.gattServer = null;
    } finally {
      this.isWorking = false;
      this.currentAction = "";
      this.syncUi();
    }
  }

  disconnectDevice() {
    if (this.bluetoothDevice && this.bluetoothDevice.gatt && this.bluetoothDevice.gatt.connected) {
      this.bluetoothDevice.gatt.disconnect();
    }
  }

  async readProductInfo() {
    if (!this.mainService) {
      return;
    }

    this.setError("");
    this.productName = "";
    this.productId = "";
    this.devicePublicKeyB64 = "";
    this.syncUi();
    try {
      this.log("Reading product info from the device...", "info");
      const [productTypeCharacteristic, productIdCharacteristic] = await Promise.all([
        this.mainService.getCharacteristic(UUIDS.productType),
        this.mainService.getCharacteristic(UUIDS.productId),
      ]);
      const [productTypeValue, productIdValue] = await Promise.all([
        productTypeCharacteristic.readValue(),
        productIdCharacteristic.readValue(),
      ]);

      this.productName = new TextDecoder("utf-8").decode(productTypeValue).trim();
      this.productId = new TextDecoder("utf-8").decode(productIdValue).trim();

      if (!this.productName) {
        throw new Error("Device reported an empty product name.");
      }
      if (!this.productId) {
        throw new Error("Device reported an empty product ID.");
      }

      this.log(`Product detected: ${this.productName} (ID ${this.productId})`, "success");

      this.log("Reading device public key chunks...", "info");
      const publicKeyTransfer = await readChunkedDevicePublicKey(this.mainService);
      this.devicePublicKeyB64 = bytesToBase64(publicKeyTransfer.bytes);
      this.log(`Device public key read successfully in ${publicKeyTransfer.totalChunks} chunks.`, "success");

      this.syncUi();
    } catch (error) {
      this.setError(error.message);
      this.syncUi();
      this.log(`Failed to read device info: ${error.message}`, "error");
    }
  }

  handleDisconnected() {
    this.productName = "";
    this.productId = "";
    this.devicePublicKeyB64 = "";
    this.mainService = null;
    this.gattServer = null;
    this.syncUi();
    this.log("Device disconnected.", "error");
  }

  async enrollAndProvision() {
    if (this.isWorking || !this.user || !this.home || !this.mainService) {
      return;
    }

    const deviceName = this.deviceNameInput.value.trim();
    if (!deviceName) {
      this.setError("Device name is required.");
      return;
    }

    if (!this.productName || !this.productId) {
      this.setError("Read the device product info before provisioning.");
      return;
    }

    if (!this.devicePublicKeyB64) {
      this.setError("Device public key was not read. Reconnect and try again.");
      return;
    }

    const productId = Number.parseInt(this.productId, 10);
    if (Number.isNaN(productId) || productId <= 0) {
      this.setError("The product ID reported by Bluetooth must be a positive integer.");
      return;
    }

    this.setError("");
    this.isWorking = true;
    this.currentAction = "provisioning";
    this.syncUi();

    try {
      this.log("Creating backend device record with encrypted provisioning...", "info");
      const device = await postJSON(
        "/api/enroll/device",
        {
          home_id: this.home.home_id,
          name: deviceName,
          product_id: productId,
          product_name: this.productName,
          device_public_key: this.devicePublicKeyB64,
        },
        this.apiBaseUrl,
      );

      this.renderSummary(device, deviceName);
      this.log(`Device record created with ID ${device.device_id}.`, "success");

      if (!device.provisioning_bundle) {
        throw new Error("Server did not return an encrypted provisioning bundle.");
      }

      this.log("Writing encrypted provisioning bundle to device...", "info");
      const bundleBytes = base64ToBytes(device.provisioning_bundle);
      const packets = buildProvisioningChunks(bundleBytes);
      const blobCharacteristic = await this.mainService.getCharacteristic(UUIDS.provisioningBlob);
      for (let i = 0; i < packets.length; i += 1) {
        await writeWithResponse(blobCharacteristic, packets[i]);
        this.log(`Provisioning chunk ${i + 1}/${packets.length} written (${packets[i].length} bytes).`, "success");
      }
      this.log(`Provisioning bundle written (${bundleBytes.length} bytes total).`, "success");

      this.log("Triggering save and restart...", "info");
      const restartCharacteristic = await this.mainService.getCharacteristic(UUIDS.restart);
      await restartCharacteristic.writeValue(new TextEncoder().encode("1"));
      this.log("Encrypted provisioning data written. Waiting for the device to reboot and reconnect to MQTT...", "info");

      const mqttStatus = await this.waitForDeviceOnline(device.device_id);
      if (!mqttStatus || !mqttStatus.mqtt_connected) {
        this.renderSummary(device, deviceName, mqttStatus);
        this.setError("Provisioning data was written, but MQTT reconnect was not observed within 60 seconds.");
        this.log("Timed out waiting for the device to reconnect to MQTT.", "error");
        return;
      }

      this.renderSummary(device, deviceName, mqttStatus);
      this.log(`Device connected to MQTT after reboot at ${formatTimestamp(mqttStatus.last_seen_at)}.`, "success");
      this.dispatchEvent(
        new CustomEvent("device-provisioned", {
          bubbles: true,
          composed: true,
          detail: {
            device,
            deviceName,
            mqttStatus,
          },
        }),
      );
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.dispatchEvent(
          new CustomEvent("session-expired", {
            bubbles: true,
            composed: true,
          }),
        );
      }
      this.setError(error.message);
      this.log(`Provisioning failed: ${error.message}`, "error");
    } finally {
      this.isWorking = false;
      this.currentAction = "";
      this.syncUi();
    }
  }

  async waitForDeviceOnline(deviceId) {
    const deadline = Date.now() + MQTT_WAIT_TIMEOUT_MS;
    let lastStatus = null;

    while (Date.now() < deadline) {
      const status = await requestJSON(`/api/enroll/device/${encodeURIComponent(deviceId)}/status`, {}, this.apiBaseUrl);
      lastStatus = status;
      if (status.mqtt_connected) {
        return status;
      }
      await sleep(MQTT_WAIT_INTERVAL_MS);
    }

    return lastStatus;
  }

  renderSummary(device, deviceName, mqttStatus = null) {
    this.summaryEl.classList.add("visible");
    this.summaryEl.innerHTML = [
      ["Device Name", deviceName],
      ["Product Name", this.productName || "Unknown"],
      ["Product ID", this.productId || "Unknown"],
      ["Device ID", device.device_id],
      ["Encrypted", device.provisioning_bundle ? "Yes" : "No"],
      ["MQTT Reconnect", formatReconnectStatus(mqttStatus)],
      ["Last MQTT Seen", formatTimestamp(mqttStatus?.last_seen_at)],
    ]
      .map(
        ([label, value]) => `
          <div class="summary-item">
            <strong>${escapeHtml(label)}</strong>
            <span>${escapeHtml(value)}</span>
          </div>
        `,
      )
      .join("");
  }

  setError(message) {
    this.errorEl.textContent = message;
  }

  log(message, type = "info") {
    const entry = document.createElement("div");
    entry.className = `log-entry ${type}`;

    const time = document.createElement("span");
    time.className = "log-time";
    time.textContent = `[${new Date().toLocaleTimeString()}]`;

    const text = document.createElement("span");
    text.className = "log-message";
    text.textContent = message;

    entry.append(time, text);
    this.logEl.append(entry);
    this.logEl.scrollTop = this.logEl.scrollHeight;
  }
}

customElements.define("device-enrollment", DeviceEnrollment);
