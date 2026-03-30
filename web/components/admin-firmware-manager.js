import { ApiError, postFormData, postJSON, requestJSON } from "../lib/api.js";
import { escapeHtml } from "../lib/html.js";
import { formatTimestamp } from "../lib/format.js";

function rolloutFormDefaults(product) {
  const percentageValue = Number.parseInt(product?.rollout_percentage ?? "0", 10);
  const percentage = percentageValue >= 1 && percentageValue <= 100 ? percentageValue : 20;

  const minutesValue = Number.parseInt(product?.rollout_interval_minutes ?? "0", 10);
  const minutes = minutesValue > 0 ? minutesValue : 60;
  if (minutes % (24 * 60) === 0) {
    return {
      percentage,
      intervalValue: Math.max(1, minutes / (24 * 60)),
      intervalUnit: "days",
    };
  }

  return {
    percentage,
    intervalValue: Math.max(1, Math.round(minutes / 60)),
    intervalUnit: "hours",
  };
}

class AdminFirmwareManager extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.apiBaseUrl = "";
    this.user = null;
    this.products = [];
    this.selectedProductId = "";
    this.errorMessage = "";
    this.successMessage = "";
    this.isLoading = true;
    this.isUploading = false;
    this.isRollingOut = false;
    this.rollouts = [];
  }

  connectedCallback() {
    this.render();
    this.loadProducts();
  }

  render() {
    const currentProduct = this.products.find((entry) => String(entry.product_id) === String(this.selectedProductId));
    const rolloutDefaults = rolloutFormDefaults(currentProduct);
    const rolloutsMarkup =
      this.rollouts.length === 0
        ? '<div class="meta">No rollouts created for this product yet.</div>'
        : this.rollouts
            .map(
              (rollout) => `
                <div class="meta">
                  <strong>Rollout #${escapeHtml(rollout.id)}</strong>
                  <div>Status: ${escapeHtml(rollout.status)}</div>
                  <div>Target: ${escapeHtml(rollout.target_version)}</div>
                  <div>Batch Size: ${escapeHtml(`${rollout.batch_percentage}%`)}</div>
                  <div>Interval: ${escapeHtml(`${rollout.batch_interval_minutes} min`)}</div>
                  <div>Next Batch: ${escapeHtml(formatTimestamp(rollout.next_batch_at, "Not uploaded"))}</div>
                  <div>Pending: ${escapeHtml(rollout.pending_count)}</div>
                  <div>Sent: ${escapeHtml(rollout.sent_count)}</div>
                  <div>Updated: ${escapeHtml(rollout.updated_count)}</div>
                  <div>Skipped: ${escapeHtml(rollout.skipped_count)}</div>
                  <div>Cancelled: ${escapeHtml(rollout.cancelled_count)}</div>
                  <div>Failed: ${escapeHtml(rollout.failed_count)}</div>
                </div>
              `,
            )
            .join("");

    this.shadowRoot.innerHTML = `
      <style>
        :host { display: block; }
        *, *::before, *::after { box-sizing: border-box; }
        .panel {
          display: grid;
          gap: 20px;
          padding: 28px;
          border-radius: 24px;
          border: 1px solid #dbeafe;
          background: #ffffff;
          box-shadow: 0 20px 44px rgba(15, 23, 42, 0.08);
        }
        .grid {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
          gap: 16px;
        }
        .meta {
          border-radius: 18px;
          border: 1px solid #e2e8f0;
          background: #f8fafc;
          padding: 18px;
        }
        label {
          display: grid;
          gap: 8px;
          font-weight: 600;
          color: #0f172a;
        }
        input, select {
          width: 100%;
          padding: 0.9rem 1rem;
          border-radius: 14px;
          border: 1px solid #cbd5e1;
          font: inherit;
          background: #ffffff;
        }
        .button-row {
          display: flex;
          gap: 12px;
          flex-wrap: wrap;
        }
        button {
          border: 0;
          border-radius: 14px;
          padding: 0.95rem 1.2rem;
          font: inherit;
          font-weight: 700;
          color: #ffffff;
          background: #2563eb;
          cursor: pointer;
        }
        button.secondary {
          color: #1d4ed8;
          background: #dbeafe;
        }
        button:disabled {
          opacity: 0.65;
          cursor: wait;
        }
        .message {
          min-height: 1.2rem;
          font-size: 0.95rem;
        }
        .message.error { color: #b91c1c; }
        .message.success { color: #166534; }
        .hint {
          color: #475569;
          line-height: 1.6;
        }
        .section {
          display: grid;
          gap: 16px;
        }
        .list {
          display: grid;
          gap: 12px;
        }
      </style>
      <section class="panel">
        <div>
          <h2>Firmware Admin</h2>
          <p class="hint">Upload firmware binaries, then create a staged retained per-device rollout using the saved product batch percentage and interval settings shown below.</p>
        </div>
        <div class="grid">
          <div class="meta">
            <strong>Signed In Admin</strong>
            <div>${escapeHtml(this.user?.username || "Unknown")}</div>
          </div>
          <div class="meta">
            <strong>Selected Product</strong>
            <div>${escapeHtml(currentProduct?.name || "Choose a product")}</div>
          </div>
          <div class="meta">
            <strong>Current Firmware</strong>
            <div>${escapeHtml(currentProduct?.firmware_version || "None")}</div>
          </div>
          <div class="meta">
            <strong>Uploaded At</strong>
            <div>${escapeHtml(formatTimestamp(currentProduct?.firmware_uploaded_at, "Not uploaded"))}</div>
          </div>
        </div>
        <form id="firmwareForm">
          <div class="grid">
            <label>
              Product
              <select id="productId" ${this.isLoading ? "disabled" : ""}>
                <option value="">Select a product</option>
                ${this.products
                  .map(
                    (product) => `
                      <option value="${escapeHtml(product.product_id)}" ${String(product.product_id) === String(this.selectedProductId) ? "selected" : ""}>
                        ${escapeHtml(product.name)} (ID ${escapeHtml(product.product_id)})
                      </option>
                    `,
                  )
                  .join("")}
              </select>
            </label>
            <label>
              Version
              <input id="version" type="text" placeholder="v1.0.2" value="${escapeHtml(currentProduct?.firmware_version || "")}" required>
            </label>
            <label>
              Firmware File
              <input id="firmwareFile" type="file" accept=".bin" required>
            </label>
          </div>
          <p class="hint">Upload the ESPHome OTA binary (<code>*.ota.bin</code>). Factory binaries (<code>*.factory.bin</code>) are rejected because HTTP OTA cannot install them.</p>
          <div class="button-row">
            <button id="uploadBtn" type="submit" ${this.isLoading ? "disabled" : ""}>${this.isUploading ? "Uploading..." : "Upload Firmware"}</button>
          </div>
        </form>
        <section class="section">
          <h3>Create Staged Rollout</h3>
          <div class="grid">
            <label>
              Batch Percentage
              <input id="batchPercentage" type="number" min="1" max="100" step="1" value="${escapeHtml(String(rolloutDefaults.percentage))}" required>
            </label>
            <label>
              Interval Value
              <input id="batchIntervalValue" type="number" min="1" step="1" value="${escapeHtml(String(rolloutDefaults.intervalValue))}" required>
            </label>
            <label>
              Interval Unit
              <select id="batchIntervalUnit">
                <option value="hours" ${rolloutDefaults.intervalUnit === "hours" ? "selected" : ""}>Hours</option>
                <option value="days" ${rolloutDefaults.intervalUnit === "days" ? "selected" : ""}>Days</option>
              </select>
            </label>
          </div>
          <div class="button-row">
            <button id="rolloutBtn" type="button" class="secondary" ${this.isLoading ? "disabled" : ""}>${this.isRollingOut ? "Creating Rollout..." : "Create Rollout"}</button>
          </div>
        </section>
        <section class="section">
          <h3>Recent Rollouts</h3>
          <div class="list">${rolloutsMarkup}</div>
        </section>
        <div class="message error">${escapeHtml(this.errorMessage)}</div>
        <div class="message success">${escapeHtml(this.successMessage)}</div>
      </section>
    `;

    this.form = this.shadowRoot.getElementById("firmwareForm");
    this.productSelect = this.shadowRoot.getElementById("productId");
    this.versionInput = this.shadowRoot.getElementById("version");
    this.batchPercentageInput = this.shadowRoot.getElementById("batchPercentage");
    this.batchIntervalValueInput = this.shadowRoot.getElementById("batchIntervalValue");
    this.batchIntervalUnitInput = this.shadowRoot.getElementById("batchIntervalUnit");
    this.fileInput = this.shadowRoot.getElementById("firmwareFile");
    this.uploadBtn = this.shadowRoot.getElementById("uploadBtn");
    this.rolloutBtn = this.shadowRoot.getElementById("rolloutBtn");

    this.productSelect?.addEventListener("change", async () => {
      this.selectedProductId = this.productSelect.value;
      this.errorMessage = "";
      this.successMessage = "";
      await this.loadRollouts();
      this.render();
    });
    this.form?.addEventListener("submit", (event) => this.handleUpload(event));
    this.rolloutBtn?.addEventListener("click", () => this.handleRollout());
  }

  async loadProducts() {
    this.isLoading = true;
    this.render();
    try {
      const response = await requestJSON("/api/admin/products", {}, this.apiBaseUrl);
      this.products = Array.isArray(response) ? response : [];
      if (!this.selectedProductId && this.products.length > 0) {
        this.selectedProductId = String(this.products[0].product_id);
      }
      await this.loadRollouts();
      this.errorMessage = "";
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.dispatchEvent(new CustomEvent("session-expired", { bubbles: true, composed: true }));
      }
      this.errorMessage = error.message;
    } finally {
      this.isLoading = false;
      this.render();
    }
  }

  async loadRollouts() {
    if (!this.selectedProductId) {
      this.rollouts = [];
      if (this.isConnected) {
        this.render();
      }
      return;
    }
    try {
      const response = await requestJSON(`/api/admin/products/${encodeURIComponent(this.selectedProductId)}/rollouts`, {}, this.apiBaseUrl);
      this.rollouts = Array.isArray(response) ? response : [];
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.dispatchEvent(new CustomEvent("session-expired", { bubbles: true, composed: true }));
      }
      this.rollouts = [];
    } finally {
      if (this.isConnected) {
        this.render();
      }
    }
  }

  async handleUpload(event) {
    event.preventDefault();
    if (this.isUploading || !this.selectedProductId) {
      return;
    }

    const file = this.fileInput?.files?.[0];
    const version = this.versionInput?.value.trim() || "";
    if (!file) {
      this.errorMessage = "Choose a firmware file to upload.";
      this.successMessage = "";
      this.render();
      return;
    }
    if (String(file.name || "").toLowerCase().endsWith(".factory.bin")) {
      this.errorMessage = "Upload the ESPHome OTA binary (*.ota.bin), not the factory binary (*.factory.bin).";
      this.successMessage = "";
      this.render();
      return;
    }
    if (!version) {
      this.errorMessage = "Enter a firmware version.";
      this.successMessage = "";
      this.render();
      return;
    }

    this.isUploading = true;
    this.errorMessage = "";
    this.successMessage = "";
    this.render();

    try {
      const formData = new FormData();
      formData.append("version", version);
      formData.append("file", file);

      const response = await postFormData(`/api/admin/products/${encodeURIComponent(this.selectedProductId)}/firmware`, formData, this.apiBaseUrl);
      this.successMessage = `Updated product ${response.product_id} to ${response.firmware_version}.`;
      this.fileInput.value = "";
      await this.loadProducts();
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.dispatchEvent(new CustomEvent("session-expired", { bubbles: true, composed: true }));
      }
      this.errorMessage = error.message;
      this.render();
    } finally {
      this.isUploading = false;
      this.render();
    }
  }

  async handleRollout() {
    if (this.isRollingOut || !this.selectedProductId) {
      return;
    }

    const rolloutPayload = {
      batch_percentage: Number.parseInt(this.batchPercentageInput?.value || "0", 10) || 0,
      batch_interval_value: Number.parseInt(this.batchIntervalValueInput?.value || "0", 10) || 0,
      batch_interval_unit: this.batchIntervalUnitInput?.value || "hours",
    };

    this.isRollingOut = true;
    this.errorMessage = "";
    this.successMessage = "";
    this.render();

    try {
      const response = await postJSON(
        `/api/admin/products/${encodeURIComponent(this.selectedProductId)}/rollout`,
        rolloutPayload,
        this.apiBaseUrl,
      );
      this.successMessage = `Created rollout #${response.rollout_id} for ${response.eligible_devices} eligible device(s) across ${response.total_batches} batch(es).`;
      await this.loadProducts();
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.dispatchEvent(new CustomEvent("session-expired", { bubbles: true, composed: true }));
      }
      this.errorMessage = error.message;
      this.render();
    } finally {
      this.isRollingOut = false;
      this.render();
    }
  }
}

customElements.define("admin-firmware-manager", AdminFirmwareManager);
