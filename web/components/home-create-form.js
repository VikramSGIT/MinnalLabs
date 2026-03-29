import { ApiError, postJSON, requestJSON } from "../lib/api.js";

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

class HomeCreateForm extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.apiBaseUrl = "";
    this.user = null;
    this.homes = [];
    this.isLoadingHomes = true;
    this.isSubmitting = false;
    this.isDeletingHome = false;
    this.selectedHomeId = "";
    this.errorMessage = "";
    this.formState = {
      name: "",
      wifiSsid: "",
      wifiPassword: "",
    };
  }

  connectedCallback() {
    this.render();
    this.loadHomes();
  }

  render() {
    const username = this.user ? this.user.username : "Unknown user";
    const existingHomesMarkup = this.isLoadingHomes
      ? '<p class="hint">Loading previously created homes...</p>'
      : this.homes.length > 0
        ? `
            <form id="existingHomeForm" class="stack">
              <label>
                Existing Homes
                <select id="existingHome" name="existingHome">
                  ${this.homes
                    .map(
                      (home) => `
                        <option value="${home.home_id}" ${String(home.home_id) === this.selectedHomeId ? "selected" : ""}>
                          ${escapeHtml(home.name)} (ID ${home.home_id})
                        </option>
                      `,
                    )
                    .join("")}
                </select>
              </label>
              <div class="actions">
                <button id="selectBtn" type="submit" class="secondary">Use Selected Home</button>
                <button id="deleteHomeBtn" type="button" class="danger" ${this.isDeletingHome ? "disabled" : ""}>
                  ${this.isDeletingHome ? "Deleting Home..." : "Delete Selected Home"}
                </button>
              </div>
            </form>
          `
        : '<p class="hint">No homes found yet. Create one below.</p>';

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

        h2 {
          margin: 0;
          font-size: 1.5rem;
        }

        p {
          margin: 12px 0 0;
          color: #475569;
          line-height: 1.6;
        }

        .stack {
          display: grid;
          gap: 18px;
        }

        .actions {
          display: flex;
          gap: 12px;
          flex-wrap: wrap;
        }

        .badge {
          display: inline-flex;
          align-items: center;
          gap: 8px;
          margin-top: 18px;
          padding: 0.45rem 0.8rem;
          border-radius: 999px;
          background: #eff6ff;
          color: #1d4ed8;
          font-weight: 600;
        }

        form {
          margin-top: 24px;
          display: grid;
          gap: 18px;
        }

        .section {
          margin-top: 24px;
          padding: 20px;
          border-radius: 18px;
          border: 1px solid #dbeafe;
          background: #f8fbff;
        }

        .section h3 {
          margin: 0;
          font-size: 1.1rem;
        }

        .section p {
          margin-top: 8px;
        }

        .separator {
          padding-top: 24px;
          border-top: 1px solid #e2e8f0;
        }

        .grid {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
          gap: 18px;
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

        select {
          width: 100%;
          padding: 0.9rem 1rem;
          border-radius: 14px;
          border: 1px solid #cbd5e1;
          font: inherit;
          color: #0f172a;
          background: #ffffff;
        }

        input:focus,
        select:focus {
          outline: 2px solid #93c5fd;
          outline-offset: 1px;
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

        button.danger {
          background: #dc2626;
          color: #ffffff;
        }

        button:disabled {
          opacity: 0.65;
          cursor: wait;
        }

        .hint {
          color: #0369a1;
          font-size: 0.95rem;
        }

        .error {
          min-height: 1.2rem;
          color: #b91c1c;
          font-size: 0.95rem;
        }
      </style>
      <section class="panel">
        <h2>Select Or Create Home</h2>
        <p>Choose an existing home or create a new one with the Wi-Fi details for that home. MQTT credentials will be generated automatically by the backend.</p>
        <div class="badge">Signed in as ${escapeHtml(username)}</div>
        <section class="section">
          <h3>Previously Created Homes</h3>
          <p>Pick a saved home if you want to enroll another device with the same credentials.</p>
          ${existingHomesMarkup}
        </section>
        <form id="homeForm">
          <div class="separator stack">
            <label>
              Home Name
              <input id="name" name="name" type="text" placeholder="Main House" required>
            </label>
            <div class="grid">
              <label>
                Wi-Fi SSID
                <input id="wifiSsid" name="wifiSsid" type="text" placeholder="Office Wi-Fi" required>
              </label>
              <label>
                Wi-Fi Password
                <input id="wifiPassword" name="wifiPassword" type="password" placeholder="Optional for open networks">
              </label>
            </div>
          </div>
          <div id="error" class="error" role="alert">${escapeHtml(this.errorMessage)}</div>
          <button id="submitBtn" type="submit">Create Home</button>
        </form>
      </section>
    `;

    this.form = this.shadowRoot.getElementById("homeForm");
    this.existingHomeForm = this.shadowRoot.getElementById("existingHomeForm");
    this.existingHomeSelect = this.shadowRoot.getElementById("existingHome");
    this.selectBtn = this.shadowRoot.getElementById("selectBtn");
    this.deleteHomeBtn = this.shadowRoot.getElementById("deleteHomeBtn");
    this.submitBtn = this.shadowRoot.getElementById("submitBtn");
    this.errorEl = this.shadowRoot.getElementById("error");
    if (this.existingHomeForm) {
      this.existingHomeForm.addEventListener("submit", (event) => this.handleExistingHomeSubmit(event));
    }
    if (this.deleteHomeBtn) {
      this.deleteHomeBtn.addEventListener("click", () => this.handleDeleteHome());
    }
    if (this.existingHomeSelect) {
      this.existingHomeSelect.addEventListener("change", () => {
        this.selectedHomeId = this.existingHomeSelect.value;
      });
    }
    this.form.addEventListener("submit", (event) => this.handleSubmit(event));
    this.form.addEventListener("input", () => this.captureFormState());
    this.syncFormState();
    this.setSubmitting(this.isSubmitting);
  }

  async handleSubmit(event) {
    event.preventDefault();
    if (this.isSubmitting || !this.user) {
      return;
    }

    this.setError("");
    this.setSubmitting(true);
    this.captureFormState();

    const payload = {
      name: this.formState.name.trim(),
      wifi_ssid: this.formState.wifiSsid.trim(),
      wifi_password: this.formState.wifiPassword,
    };

    try {
      const response = await postJSON("/api/enroll/home", payload, this.apiBaseUrl);
      this.dispatchEvent(
        new CustomEvent("home-created", {
          bubbles: true,
          composed: true,
          detail: response,
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
    } finally {
      this.setSubmitting(false);
    }
  }

  async loadHomes() {
    this.isLoadingHomes = true;
    this.render();

    try {
      const response = await requestJSON("/api/enroll/homes", {}, this.apiBaseUrl);
      this.homes = Array.isArray(response) ? response : [];
      const selectedExists = this.homes.some((home) => String(home.home_id) === String(this.selectedHomeId));
      if (!selectedExists) {
        this.selectedHomeId = this.homes.length > 0 ? String(this.homes[0].home_id) : "";
      }
      this.setError("");
    } catch (error) {
      this.homes = [];
      if (error instanceof ApiError && error.status === 401) {
        this.dispatchEvent(
          new CustomEvent("session-expired", {
            bubbles: true,
            composed: true,
          }),
        );
      }
      this.setError(error.message);
    } finally {
      this.isLoadingHomes = false;
      this.render();
    }
  }

  handleExistingHomeSubmit(event) {
    event.preventDefault();
    if (this.isLoadingHomes || this.homes.length === 0) {
      return;
    }

    const home = this.homes.find((entry) => String(entry.home_id) === String(this.selectedHomeId || this.existingHomeSelect?.value || ""));
    if (!home) {
      this.setError("Select a home to continue.");
      return;
    }

    this.setError("");
    this.dispatchEvent(
      new CustomEvent("home-selected", {
        bubbles: true,
        composed: true,
        detail: home,
      }),
    );
  }

  async handleDeleteHome() {
    if (this.isLoadingHomes || this.isDeletingHome || this.homes.length === 0) {
      return;
    }

    const home = this.homes.find((entry) => String(entry.home_id) === String(this.selectedHomeId || this.existingHomeSelect?.value || ""));
    if (!home) {
      this.setError("Select a home to delete.");
      return;
    }

    const confirmed = window.confirm(
      `Delete home "${home.name}" permanently? All devices in this home will also be deleted.`,
    );
    if (!confirmed) {
      return;
    }

    this.setError("");
    this.isDeletingHome = true;
    this.setSubmitting(this.isSubmitting);

    try {
      await requestJSON(`/api/enroll/home/${encodeURIComponent(home.home_id)}`, { method: "DELETE" }, this.apiBaseUrl);
      await this.loadHomes();
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
    } finally {
      this.isDeletingHome = false;
      this.setSubmitting(this.isSubmitting);
    }
  }

  captureFormState() {
    this.formState = {
      name: this.shadowRoot.getElementById("name")?.value || "",
      wifiSsid: this.shadowRoot.getElementById("wifiSsid")?.value || "",
      wifiPassword: this.shadowRoot.getElementById("wifiPassword")?.value || "",
    };
  }

  syncFormState() {
    const name = this.shadowRoot.getElementById("name");
    const wifiSsid = this.shadowRoot.getElementById("wifiSsid");
    const wifiPassword = this.shadowRoot.getElementById("wifiPassword");

    if (name) {
      name.value = this.formState.name;
    }
    if (wifiSsid) {
      wifiSsid.value = this.formState.wifiSsid;
    }
    if (wifiPassword) {
      wifiPassword.value = this.formState.wifiPassword;
    }
  }

  setSubmitting(isSubmitting) {
    this.isSubmitting = isSubmitting;
    this.submitBtn.disabled = isSubmitting;
    this.submitBtn.textContent = isSubmitting ? "Creating Home..." : "Create Home";
    if (this.selectBtn) {
      this.selectBtn.disabled = isSubmitting || this.isLoadingHomes || this.isDeletingHome || this.homes.length === 0;
    }
    if (this.existingHomeSelect) {
      this.existingHomeSelect.disabled = isSubmitting || this.isLoadingHomes || this.isDeletingHome || this.homes.length === 0;
    }
    if (this.deleteHomeBtn) {
      this.deleteHomeBtn.disabled = isSubmitting || this.isLoadingHomes || this.isDeletingHome || this.homes.length === 0;
      this.deleteHomeBtn.textContent = this.isDeletingHome ? "Deleting Home..." : "Delete Selected Home";
    }
  }

  setError(message) {
    this.errorMessage = message;
    if (this.errorEl) {
      this.errorEl.textContent = message;
    }
  }
}

customElements.define("home-create-form", HomeCreateForm);
