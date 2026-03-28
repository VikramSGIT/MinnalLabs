import { ApiError, postJSON } from "../lib/api.js";

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
    this.isSubmitting = false;
  }

  connectedCallback() {
    this.render();
  }

  render() {
    const username = this.user ? this.user.username : "Unknown user";

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

        input:focus {
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

        button:disabled {
          opacity: 0.65;
          cursor: wait;
        }

        .error {
          min-height: 1.2rem;
          color: #b91c1c;
          font-size: 0.95rem;
        }
      </style>
      <section class="panel">
        <h2>Create Home</h2>
        <p>Store the Wi-Fi network and the MQTT username/password that should be issued to devices for this home.</p>
        <div class="badge">Signed in as ${escapeHtml(username)}</div>
        <form id="homeForm">
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
          <div class="grid">
            <label>
              MQTT Username
              <input id="mqttUsername" name="mqttUsername" type="text" placeholder="Provided by you" required>
            </label>
            <label>
              MQTT Password
              <input id="mqttPassword" name="mqttPassword" type="password" placeholder="Provided by you" required>
            </label>
          </div>
          <div id="error" class="error" role="alert"></div>
          <button id="submitBtn" type="submit">Create Home</button>
        </form>
      </section>
    `;

    this.form = this.shadowRoot.getElementById("homeForm");
    this.submitBtn = this.shadowRoot.getElementById("submitBtn");
    this.errorEl = this.shadowRoot.getElementById("error");
    this.form.addEventListener("submit", (event) => this.handleSubmit(event));
  }

  async handleSubmit(event) {
    event.preventDefault();
    if (this.isSubmitting || !this.user) {
      return;
    }

    this.setError("");
    this.setSubmitting(true);

    const payload = {
      name: this.shadowRoot.getElementById("name").value.trim(),
      wifi_ssid: this.shadowRoot.getElementById("wifiSsid").value.trim(),
      wifi_password: this.shadowRoot.getElementById("wifiPassword").value,
      mqtt_username: this.shadowRoot.getElementById("mqttUsername").value.trim(),
      mqtt_password: this.shadowRoot.getElementById("mqttPassword").value,
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

  setSubmitting(isSubmitting) {
    this.isSubmitting = isSubmitting;
    this.submitBtn.disabled = isSubmitting;
    this.submitBtn.textContent = isSubmitting ? "Creating Home..." : "Create Home";
  }

  setError(message) {
    this.errorEl.textContent = message;
  }
}

customElements.define("home-create-form", HomeCreateForm);
