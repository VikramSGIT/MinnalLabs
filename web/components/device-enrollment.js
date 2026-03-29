import { ApiError, postJSON, requestJSON } from "../lib/api.js";

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

const SERVICE_UUID = "12345678-1234-1234-1234-000000000000";
const UUIDS = {
  productId: "12345678-1234-1234-1234-000000000001",
  userId: "12345678-1234-1234-1234-000000000002",
  deviceId: "12345678-1234-1234-1234-000000000003",
  mqttHost: "12345678-1234-1234-1234-000000000004",
  wifiSsid: "12345678-1234-1234-1234-000000000005",
  wifiPassword: "12345678-1234-1234-1234-000000000006",
  mqttUsername: "12345678-1234-1234-1234-000000000007",
  mqttPassword: "12345678-1234-1234-1234-000000000008",
  mqttPort: "12345678-1234-1234-1234-000000000009",
  homeId: "12345678-1234-1234-1234-00000000000a",
  restart: "12345678-1234-1234-1234-00000000000b",
};

const MQTT_WAIT_TIMEOUT_MS = 60_000;
const MQTT_WAIT_INTERVAL_MS = 2_000;

function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function formatTimestamp(value) {
  if (!value) {
    return "Not seen yet";
  }

  const timestamp = new Date(value);
  if (Number.isNaN(timestamp.getTime())) {
    return "Not seen yet";
  }

  return timestamp.toLocaleString();
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
    this.productId = "";
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
          <p>Read the device product ID over Bluetooth, create the backend device record, then write backend-issued IDs plus Wi-Fi and MQTT settings to the device.</p>
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
            <button id="readBtn" type="button" disabled>Read Product ID</button>
          </div>
        </div>

        <div class="section">
          <div class="grid">
            <label>
              Product ID
              <input id="productIdInput" type="text" readonly placeholder="Read from Bluetooth">
            </label>
            <label>
              Device Name
              <input id="deviceNameInput" type="text" placeholder="Kitchen Sensor" required>
            </label>
          </div>
          <p class="note">This flow writes <code>user_id</code>, <code>home_id</code>, <code>device_id</code>, <code>mqtt_host</code>, <code>mqtt_port</code>, Wi-Fi SSID/password, and MQTT username/password to the device.</p>
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
    this.productIdInput = this.shadowRoot.getElementById("productIdInput");
    this.deviceNameInput = this.shadowRoot.getElementById("deviceNameInput");
    this.errorEl = this.shadowRoot.getElementById("error");
    this.logEl = this.shadowRoot.getElementById("log");
    this.summaryEl = this.shadowRoot.getElementById("summary");
  }

  bindEvents() {
    this.connectBtn.addEventListener("click", () => this.connectDevice());
    this.disconnectBtn.addEventListener("click", () => this.disconnectDevice());
    this.readBtn.addEventListener("click", () => this.readProductId());
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

      await this.readProductId();
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

  async readProductId() {
    if (!this.mainService) {
      return;
    }

    this.setError("");
    try {
      this.log("Reading product ID from the device...", "info");
      const characteristic = await this.mainService.getCharacteristic(UUIDS.productId);
      const value = await characteristic.readValue();
      this.productId = new TextDecoder("utf-8").decode(value).trim();
      this.productIdInput.value = this.productId;
      this.log(`Product ID detected: ${this.productId}`, "success");
    } catch (error) {
      this.setError(error.message);
      this.log(`Failed to read product ID: ${error.message}`, "error");
    }
  }

  handleDisconnected() {
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

    if (!this.productId) {
      this.setError("Read the device product ID before provisioning.");
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
      this.log("Creating backend device record...", "info");
      const device = await postJSON(
        "/api/enroll/device",
        {
          home_id: this.home.home_id,
          name: deviceName,
          product_id: productId,
        },
        this.apiBaseUrl,
      );

      this.renderSummary(device, deviceName);
      this.log(`Device record created with ID ${device.device_id}.`, "success");

      const fields = [
        { label: "User ID", uuid: UUIDS.userId, value: String(device.user_id) },
        { label: "Device ID", uuid: UUIDS.deviceId, value: String(device.device_id) },
        { label: "MQTT Host", uuid: UUIDS.mqttHost, value: String(device.mqtt_host) },
        { label: "Wi-Fi SSID", uuid: UUIDS.wifiSsid, value: String(device.wifi_ssid || "") },
        { label: "Wi-Fi Password", uuid: UUIDS.wifiPassword, value: String(device.wifi_password || "") },
        { label: "MQTT Username", uuid: UUIDS.mqttUsername, value: String(device.mqtt_username || "") },
        { label: "MQTT Password", uuid: UUIDS.mqttPassword, value: String(device.mqtt_password || "") },
        { label: "MQTT Port", uuid: UUIDS.mqttPort, value: String(device.mqtt_port) },
        { label: "Home ID", uuid: UUIDS.homeId, value: String(device.home_id) },
      ];

      let writes = 0;
      for (const field of fields) {
        if (!field.value) {
          this.log(`${field.label} skipped because the value is empty.`, "info");
          continue;
        }

        const characteristic = await this.mainService.getCharacteristic(field.uuid);
        await characteristic.writeValue(new TextEncoder().encode(field.value));
        writes += 1;
        this.log(`${field.label} written to device.`, "success");
      }

      if (writes === 0) {
        throw new Error("Nothing was written to the device.");
      }

      this.log("Triggering save and restart...", "info");
      const restartCharacteristic = await this.mainService.getCharacteristic(UUIDS.restart);
      await restartCharacteristic.writeValue(new TextEncoder().encode("1"));
      this.log("Provisioning data written. Waiting for the device to reboot and reconnect to MQTT...", "info");

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
      ["Device ID", device.device_id],
      ["User ID", device.user_id],
      ["Home ID", device.home_id],
      ["MQTT Host", device.mqtt_host],
      ["MQTT Port", device.mqtt_port],
      ["MQTT Reconnect", formatReconnectStatus(mqttStatus)],
      ["Last MQTT Seen", formatTimestamp(mqttStatus?.last_seen_at)],
      ["MQTT Username", device.mqtt_username || "Not set"],
      ["Wi-Fi SSID", device.wifi_ssid || "Not set"],
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
