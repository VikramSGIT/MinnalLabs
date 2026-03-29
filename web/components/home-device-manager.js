import { ApiError, requestJSON } from "../lib/api.js";
import "./device-enrollment.js";

const DEVICE_POLL_INTERVAL_MS = 5000;

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
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

function statusLabel(device) {
  if (device.mqtt_connected) {
    return "Online";
  }

  if (device.mqtt_status === "offline") {
    return "Offline";
  }

  return "Unknown";
}

function statusClass(device) {
  if (device.mqtt_connected) {
    return "online";
  }

  if (device.mqtt_status === "offline") {
    return "offline";
  }

  return "unknown";
}

function statusMeta(device) {
  if (device.mqtt_connected && device.last_seen_at) {
    return `Connected at ${formatTimestamp(device.last_seen_at)}`;
  }

  if (device.last_seen_at) {
    return `Last seen ${formatTimestamp(device.last_seen_at)}`;
  }

  if (device.last_status_at) {
    return `Last status update ${formatTimestamp(device.last_status_at)}`;
  }

  return "No MQTT status seen yet";
}

class HomeDeviceManager extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.apiBaseUrl = "";
    this.user = null;
    this.home = null;
    this.activeTab = "devices";
    this.devices = [];
    this.devicesError = "";
    this.isLoadingDevices = true;
    this.isRefreshingDevices = false;
    this.devicePollTimer = null;
    this.isFetchingDevices = false;
  }

  connectedCallback() {
    this.render();
    this.loadDevices();
    this.syncPolling();
  }

  disconnectedCallback() {
    this.stopPolling();
  }

  render() {
    const onlineCount = this.devices.filter((device) => device.mqtt_connected).length;
    const deviceCount = this.devices.length;

    this.shadowRoot.innerHTML = `
      <style>
        :host {
          display: block;
        }

        *, *::before, *::after {
          box-sizing: border-box;
        }

        .panel {
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
          margin: 12px 0 0;
          color: #475569;
          line-height: 1.6;
        }

        .tab-bar {
          display: inline-grid;
          grid-template-columns: repeat(2, minmax(0, 1fr));
          gap: 8px;
          margin-top: 24px;
          padding: 6px;
          border-radius: 16px;
          background: #eff6ff;
          border: 1px solid #dbeafe;
        }

        .tab-btn {
          border: 0;
          border-radius: 12px;
          padding: 0.8rem 1rem;
          font: inherit;
          font-weight: 700;
          color: #1d4ed8;
          background: transparent;
          cursor: pointer;
        }

        .tab-btn.active {
          color: #ffffff;
          background: #2563eb;
        }

        .tab-btn:disabled {
          opacity: 0.65;
          cursor: wait;
        }

        .content {
          margin-top: 24px;
        }

        .cards {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
          gap: 16px;
          margin-bottom: 20px;
        }

        .card {
          padding: 18px;
          border-radius: 18px;
          border: 1px solid #e2e8f0;
          background: #f8fafc;
        }

        .eyebrow {
          margin: 0 0 8px;
          color: #64748b;
          font-size: 0.85rem;
          text-transform: uppercase;
          letter-spacing: 0.08em;
        }

        .value {
          margin: 0;
          color: #0f172a;
          font-size: 1.25rem;
          font-weight: 700;
        }

        .toolbar {
          display: flex;
          justify-content: space-between;
          align-items: center;
          gap: 12px;
          flex-wrap: wrap;
          margin-bottom: 20px;
        }

        .toolbar-text {
          margin: 0;
          color: #475569;
          font-size: 0.95rem;
        }

        .refresh-btn {
          border: 0;
          border-radius: 999px;
          padding: 0.8rem 1.2rem;
          font: inherit;
          font-weight: 700;
          cursor: pointer;
          color: #1d4ed8;
          background: #dbeafe;
        }

        .refresh-btn:disabled {
          opacity: 0.65;
          cursor: wait;
        }

        .error {
          margin-bottom: 18px;
          padding: 14px 16px;
          border-radius: 16px;
          border: 1px solid #fecaca;
          background: #fef2f2;
          color: #b91c1c;
          font-weight: 600;
        }

        .empty {
          padding: 24px;
          border-radius: 18px;
          border: 1px dashed #cbd5e1;
          background: #f8fafc;
          text-align: center;
        }

        .empty p {
          margin: 8px 0 0;
        }

        .list {
          display: grid;
          gap: 14px;
        }

        .device {
          display: grid;
          gap: 14px;
          padding: 18px;
          border-radius: 18px;
          border: 1px solid #e2e8f0;
          background: #f8fafc;
        }

        .device-header {
          display: flex;
          justify-content: space-between;
          align-items: start;
          gap: 12px;
          flex-wrap: wrap;
        }

        .device-title {
          margin: 0;
          color: #0f172a;
          font-size: 1.05rem;
          font-weight: 700;
        }

        .device-subtitle {
          margin: 6px 0 0;
          color: #64748b;
          font-size: 0.92rem;
        }

        .badge {
          display: inline-flex;
          align-items: center;
          gap: 8px;
          padding: 0.45rem 0.8rem;
          border-radius: 999px;
          font-size: 0.92rem;
          font-weight: 700;
        }

        .badge.online {
          color: #166534;
          background: #dcfce7;
        }

        .badge.offline {
          color: #991b1b;
          background: #fee2e2;
        }

        .badge.unknown {
          color: #92400e;
          background: #fef3c7;
        }

        .meta {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
          gap: 12px;
        }

        .meta-item {
          padding: 12px 14px;
          border-radius: 14px;
          border: 1px solid #e2e8f0;
          background: #ffffff;
        }

        .meta-item strong {
          display: block;
          margin-bottom: 6px;
          color: #64748b;
          font-size: 0.8rem;
          text-transform: uppercase;
          letter-spacing: 0.08em;
        }

        .meta-item span {
          color: #0f172a;
          word-break: break-word;
        }
      </style>
      <section class="panel">
        <div>
          <h2>Home Devices</h2>
          <p>Review devices already assigned to this home, check whether they are online, or add a new device over Web Bluetooth.</p>
        </div>
        <div class="tab-bar" role="tablist" aria-label="Home Device Tabs">
          <button id="devicesTabBtn" type="button" class="tab-btn ${this.activeTab === "devices" ? "active" : ""}" aria-selected="${this.activeTab === "devices"}">Devices</button>
          <button id="addDeviceTabBtn" type="button" class="tab-btn ${this.activeTab === "add" ? "active" : ""}" aria-selected="${this.activeTab === "add"}">Add Device</button>
        </div>
        <div id="content" class="content"></div>
      </section>
    `;

    this.shadowRoot.getElementById("devicesTabBtn").addEventListener("click", () => this.setActiveTab("devices"));
    this.shadowRoot.getElementById("addDeviceTabBtn").addEventListener("click", () => this.setActiveTab("add"));

    const content = this.shadowRoot.getElementById("content");
    if (this.activeTab === "devices") {
      content.innerHTML = this.renderDevicesMarkup(deviceCount, onlineCount);
      const refreshBtn = this.shadowRoot.getElementById("refreshDevicesBtn");
      if (refreshBtn) {
        refreshBtn.addEventListener("click", () => this.loadDevices({ forceVisibleLoading: true }));
      }
    } else {
      const enrollment = document.createElement("device-enrollment");
      enrollment.apiBaseUrl = this.apiBaseUrl;
      enrollment.user = this.user;
      enrollment.home = this.home;
      enrollment.addEventListener("device-provisioned", (event) => this.handleDeviceProvisioned(event));
      content.append(enrollment);
    }
  }

  renderDevicesMarkup(deviceCount, onlineCount) {
    if (this.isLoadingDevices && deviceCount === 0) {
      return `
        <div class="empty">
          <h3>Loading Devices</h3>
          <p>Fetching devices for the selected home...</p>
        </div>
      `;
    }

    const listMarkup =
      deviceCount === 0
        ? `
          <div class="empty">
            <h3>No Devices Yet</h3>
            <p>This home does not have any enrolled devices yet. Open the Add Device tab to provision one.</p>
          </div>
        `
        : `
          <div class="list">
            ${this.devices
              .map(
                (device) => `
                  <article class="device">
                    <div class="device-header">
                      <div>
                        <h3 class="device-title">${escapeHtml(device.name || "Unnamed Device")}</h3>
                        <p class="device-subtitle">Device ID ${escapeHtml(device.device_id)} · Product ${escapeHtml(device.product_id)}</p>
                      </div>
                      <span class="badge ${statusClass(device)}">${statusLabel(device)}</span>
                    </div>
                    <div class="meta">
                      <div class="meta-item">
                        <strong>MQTT Status</strong>
                        <span>${escapeHtml(device.mqtt_status || "unknown")}</span>
                      </div>
                      <div class="meta-item">
                        <strong>Last Seen</strong>
                        <span>${escapeHtml(statusMeta(device))}</span>
                      </div>
                      <div class="meta-item">
                        <strong>Added</strong>
                        <span>${escapeHtml(formatTimestamp(device.created_at))}</span>
                      </div>
                    </div>
                  </article>
                `,
              )
              .join("")}
          </div>
        `;

    return `
      <div class="cards">
        <article class="card">
          <p class="eyebrow">Total Devices</p>
          <p class="value">${deviceCount}</p>
        </article>
        <article class="card">
          <p class="eyebrow">Online Now</p>
          <p class="value">${onlineCount}</p>
        </article>
      </div>
      <div class="toolbar">
        <p class="toolbar-text">${this.isRefreshingDevices ? "Refreshing statuses..." : "Statuses auto-refresh while this tab is open."}</p>
        <button id="refreshDevicesBtn" type="button" class="refresh-btn" ${this.isFetchingDevices ? "disabled" : ""}>${this.isFetchingDevices ? "Refreshing..." : "Refresh Devices"}</button>
      </div>
      ${this.devicesError ? `<div class="error">${escapeHtml(this.devicesError)}</div>` : ""}
      ${listMarkup}
    `;
  }

  setActiveTab(tab) {
    if (this.activeTab === tab) {
      return;
    }

    this.activeTab = tab;
    if (tab === "devices") {
      this.render();
      this.loadDevices();
      this.syncPolling();
      return;
    }

    this.stopPolling();
    this.render();
  }

  syncPolling() {
    if (this.activeTab === "devices") {
      this.startPolling();
      return;
    }

    this.stopPolling();
  }

  startPolling() {
    if (this.devicePollTimer) {
      return;
    }

    this.devicePollTimer = window.setInterval(() => {
      this.loadDevices();
    }, DEVICE_POLL_INTERVAL_MS);
  }

  stopPolling() {
    if (!this.devicePollTimer) {
      return;
    }

    window.clearInterval(this.devicePollTimer);
    this.devicePollTimer = null;
  }

  async loadDevices({ forceVisibleLoading = false } = {}) {
    if (!this.home || !this.home.home_id || this.isFetchingDevices) {
      return;
    }

    this.isFetchingDevices = true;
    if (this.devices.length === 0 || forceVisibleLoading) {
      this.isLoadingDevices = true;
    } else {
      this.isRefreshingDevices = true;
    }
    if (this.activeTab === "devices") {
      this.render();
    }

    try {
      const response = await requestJSON(`/api/enroll/home/${encodeURIComponent(this.home.home_id)}/devices`, {}, this.apiBaseUrl);
      this.devices = Array.isArray(response) ? response : [];
      this.devicesError = "";
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.dispatchEvent(
          new CustomEvent("session-expired", {
            bubbles: true,
            composed: true,
          }),
        );
      }
      this.devicesError = error.message;
    } finally {
      this.isFetchingDevices = false;
      this.isLoadingDevices = false;
      this.isRefreshingDevices = false;
      if (this.isConnected && this.activeTab === "devices") {
        this.render();
      }
    }
  }

  async handleDeviceProvisioned() {
    this.activeTab = "devices";
    this.render();
    this.syncPolling();
    await this.loadDevices({ forceVisibleLoading: true });
  }
}

customElements.define("home-device-manager", HomeDeviceManager);
