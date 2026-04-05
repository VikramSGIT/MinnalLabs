import { getFrontendConfig } from "../lib/config.js";
import { ApiError, requestJSON } from "../lib/api.js";
import { escapeHtml } from "../lib/html.js";
import "./admin-firmware-manager.js";
import "./home-device-manager.js";
import "./home-create-form.js";
import "./kratos-flow.js";

const AUTH_ROUTES = new Set(["login", "registration", "verification"]);

class AppShell extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.config = getFrontendConfig();
    this.state = {
      user: null,
      home: null,
      page: "enrollment",
      route: "app",
      isBootstrapping: true,
      sessionError: "",
    };
    this.sessionEventsBound = false;
  }

  connectedCallback() {
    if (!this.sessionEventsBound) {
      this.shadowRoot.addEventListener("session-expired", () => {
        window.location.href = "/self-service/login/browser";
      });
      window.addEventListener("popstate", () => {
        this.resolveRoute();
        this.render();
      });
      this.sessionEventsBound = true;
    }

    this.resolveRoute();
    this.render();

    if (AUTH_ROUTES.has(this.state.route)) {
      this.state.isBootstrapping = false;
      this.render();
    } else if (this.state.route === "settings") {
      this.bootstrapSession().then(() => {
        if (!this.state.user) {
          window.location.href = "/self-service/login/browser?return_to=/settings";
        }
      });
    } else {
      this.bootstrapSession();
    }
  }

  resolveRoute() {
    const path = window.location.pathname.replace(/^\/+/, "");
    if (["login", "registration", "verification", "settings"].includes(path)) {
      this.state.route = path;
    } else {
      this.state.route = "app";
    }
  }

  render() {
    const { user, home, page, isBootstrapping, sessionError, route } = this.state;

    // Auth flow routes get a minimal shell
    if (AUTH_ROUTES.has(route) || route === "settings") {
      this.shadowRoot.innerHTML = `
        <style>
          :host {
            display: block;
            box-sizing: border-box;
            min-height: 100vh;
            padding: 32px 20px;
          }
          *, *::before, *::after { box-sizing: border-box; }
          .shell {
            width: min(480px, 100%);
            margin: 0 auto;
            display: grid;
            gap: 24px;
          }
          h1 {
            margin: 0;
            font-size: 1.8rem;
            text-align: center;
          }
        </style>
        <div class="shell">
          <h1>IoT Platform</h1>
          <div id="stage"></div>
        </div>
      `;
      this.renderStage();
      return;
    }

    const steps = [
      { label: "Login", status: user ? "complete" : "active" },
      { label: "Choose Home", status: user && page !== "admin" && !home ? "active" : home ? "complete" : "pending" },
      { label: "Manage Devices", status: page === "admin" ? "pending" : home ? "active" : "pending" },
    ];

    this.shadowRoot.innerHTML = `
      <style>
        :host {
          display: block;
          box-sizing: border-box;
          min-height: 100vh;
          padding: 32px 20px;
        }

        *, *::before, *::after {
          box-sizing: border-box;
        }

        .shell {
          width: min(1080px, 100%);
          margin: 0 auto;
          display: grid;
          gap: 24px;
        }

        .hero {
          display: grid;
          gap: 20px;
          padding: 28px;
          border: 1px solid rgba(148, 163, 184, 0.3);
          border-radius: 24px;
          background: rgba(255, 255, 255, 0.9);
          box-shadow: 0 24px 48px rgba(15, 23, 42, 0.08);
          backdrop-filter: blur(12px);
        }

        .hero-row {
          display: flex;
          justify-content: space-between;
          gap: 16px;
          align-items: start;
          flex-wrap: wrap;
        }

        h1 {
          margin: 0;
          font-size: clamp(1.8rem, 3vw, 2.8rem);
          line-height: 1.1;
        }

        .subtitle {
          margin: 10px 0 0;
          max-width: 720px;
          color: #475569;
          line-height: 1.6;
        }

        .actions {
          display: flex;
          gap: 12px;
          flex-wrap: wrap;
        }

        button {
          border: 0;
          border-radius: 999px;
          padding: 0.8rem 1.2rem;
          font: inherit;
          font-weight: 600;
          cursor: pointer;
          color: #0f172a;
          background: #e2e8f0;
        }

        button.primary {
          color: #ffffff;
          background: #2563eb;
        }

        .meta {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
          gap: 16px;
        }

        .card {
          padding: 18px;
          border-radius: 18px;
          background: #f8fafc;
          border: 1px solid #e2e8f0;
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
          font-size: 1rem;
          line-height: 1.5;
          color: #0f172a;
          word-break: break-word;
        }

        .banner {
          padding: 14px 18px;
          border-radius: 16px;
          border: 1px solid #fecaca;
          background: #fef2f2;
          color: #b91c1c;
          font-weight: 600;
        }

        .panel {
          padding: 28px;
          border-radius: 24px;
          border: 1px solid #dbeafe;
          background: #ffffff;
          box-shadow: 0 20px 44px rgba(15, 23, 42, 0.08);
          color: #0f172a;
        }

        .steps {
          display: grid;
          grid-template-columns: repeat(3, minmax(0, 1fr));
          gap: 12px;
        }

        .step {
          display: flex;
          align-items: center;
          gap: 12px;
          padding: 14px 16px;
          border-radius: 18px;
          border: 1px solid #dbeafe;
          background: #eff6ff;
          color: #1d4ed8;
          font-weight: 600;
        }

        .step.pending {
          border-color: #e2e8f0;
          background: #f8fafc;
          color: #64748b;
        }

        .step.complete {
          border-color: #bbf7d0;
          background: #f0fdf4;
          color: #15803d;
        }

        .step-badge {
          width: 28px;
          height: 28px;
          border-radius: 999px;
          display: inline-flex;
          align-items: center;
          justify-content: center;
          background: rgba(255, 255, 255, 0.7);
          border: 1px solid currentColor;
          font-size: 0.9rem;
          flex: 0 0 auto;
        }

        #stage {
          min-height: 360px;
        }

        @media (max-width: 720px) {
          .steps {
            grid-template-columns: 1fr;
          }
        }
      </style>
      <div class="shell">
        <section class="hero">
          <div class="hero-row">
            <div>
              <h1>IoT Enrollment</h1>
              <p class="subtitle">
                Sign in, choose or create a home with Wi-Fi and MQTT credentials, then manage devices and provision new ones over Web Bluetooth.
              </p>
            </div>
            <div class="actions">
              ${user ? '<button id="settingsBtn">Settings</button>' : ""}
              ${user ? '<button id="signOutBtn">Sign Out</button>' : ""}
              ${user && user.is_admin && page !== "admin" ? '<button id="openAdminBtn" class="primary">Firmware Admin</button>' : ""}
              ${user && user.is_admin && page === "admin" ? '<button id="closeAdminBtn" class="primary">Back To Enrollment</button>' : ""}
              ${page !== "admin" && home ? '<button id="resetHomeBtn" class="primary">Change Home</button>' : ""}
            </div>
          </div>
          <div class="meta">
            <article class="card">
              <p class="eyebrow">Signed In User</p>
              <p class="value">${user ? `${escapeHtml(user.username)} (ID ${user.user_id})` : "Not signed in yet"}</p>
            </article>
            <article class="card">
              <p class="eyebrow">Current Home</p>
              <p class="value">${page === "admin" ? "Firmware Admin" : home ? `${escapeHtml(home.name)} (ID ${home.home_id})` : "Select or create a home to continue"}</p>
            </article>
          </div>
          <div class="steps">
            ${steps
              .map(
                (step, index) => `
                  <div class="step ${step.status}">
                    <span class="step-badge">${index + 1}</span>
                    <span>${step.label}</span>
                  </div>
                `,
              )
              .join("")}
          </div>
        </section>
        ${sessionError ? `<div class="banner">${escapeHtml(sessionError)}</div>` : ""}
        <div id="stage"></div>
      </div>
    `;

    const settingsBtn = this.shadowRoot.getElementById("settingsBtn");
    if (settingsBtn) {
      settingsBtn.addEventListener("click", () => {
        window.location.href = "/self-service/settings/browser";
      });
    }

    const signOutBtn = this.shadowRoot.getElementById("signOutBtn");
    if (signOutBtn) {
      signOutBtn.addEventListener("click", () => this.handleSignOut());
    }

    const resetHomeBtn = this.shadowRoot.getElementById("resetHomeBtn");
    if (resetHomeBtn) {
      resetHomeBtn.addEventListener("click", () => {
        this.state.home = null;
        this.state.page = "enrollment";
        this.render();
      });
    }

    const openAdminBtn = this.shadowRoot.getElementById("openAdminBtn");
    if (openAdminBtn) {
      openAdminBtn.addEventListener("click", () => {
        this.state.page = "admin";
        this.render();
      });
    }

    const closeAdminBtn = this.shadowRoot.getElementById("closeAdminBtn");
    if (closeAdminBtn) {
      closeAdminBtn.addEventListener("click", () => {
        this.state.page = "enrollment";
        this.render();
      });
    }

    this.renderStage();
  }

  renderStage() {
    const stage = this.shadowRoot.getElementById("stage");
    stage.innerHTML = "";

    // Kratos auth flow routes
    if (AUTH_ROUTES.has(this.state.route) || this.state.route === "settings") {
      const kratosFlow = document.createElement("kratos-flow");
      kratosFlow.setAttribute("flow-type", this.state.route);
      stage.append(kratosFlow);
      return;
    }

    if (this.state.isBootstrapping) {
      stage.innerHTML = `
        <section class="panel">
          <h2>Checking Session</h2>
          <p>Loading the current authenticated session...</p>
        </section>
      `;
      return;
    }

    if (!this.state.user) {
      window.location.href = "/self-service/login/browser";
      return;
    }

    if (this.state.user.is_admin && this.state.page === "admin") {
      const adminFirmwareManager = document.createElement("admin-firmware-manager");
      adminFirmwareManager.apiBaseUrl = this.config.apiBaseUrl;
      adminFirmwareManager.user = this.state.user;
      stage.append(adminFirmwareManager);
      return;
    }

    if (!this.state.home) {
      const homeForm = document.createElement("home-create-form");
      homeForm.apiBaseUrl = this.config.apiBaseUrl;
      homeForm.user = this.state.user;
      const handleHomeSelection = (event) => {
        this.state.home = event.detail;
        this.state.page = "enrollment";
        this.render();
      };
      homeForm.addEventListener("home-created", handleHomeSelection);
      homeForm.addEventListener("home-selected", handleHomeSelection);
      stage.append(homeForm);
      return;
    }

    const homeDeviceManager = document.createElement("home-device-manager");
    homeDeviceManager.apiBaseUrl = this.config.apiBaseUrl;
    homeDeviceManager.user = this.state.user;
    homeDeviceManager.home = this.state.home;
    homeDeviceManager.addEventListener("home-deleted", () => {
      this.state.home = null;
      this.state.page = "enrollment";
      this.render();
    });
    stage.append(homeDeviceManager);
  }

  async bootstrapSession() {
    this.state.isBootstrapping = true;
    this.render();

    try {
      const user = await requestJSON("/api/session/me", {}, this.config.apiBaseUrl);
      this.state.user = user;
      this.state.page = "enrollment";
      this.state.sessionError = "";
    } catch (error) {
      this.state.user = null;
      if (error instanceof ApiError && error.status === 403 && error.payload && error.payload.error === "email_not_verified") {
        window.location.href = "/self-service/verification/browser";
        return;
      }
      if (!(error instanceof ApiError && error.status === 401)) {
        this.state.sessionError = error.message;
      }
    } finally {
      this.state.isBootstrapping = false;
      this.render();
    }
  }

  async handleSignOut() {
    try {
      const resp = await fetch("/self-service/logout/browser", {
        headers: { Accept: "application/json" },
        credentials: "include",
      });
      if (!resp.ok) throw new Error("Failed to create logout flow");
      const data = await resp.json();
      window.location.href = data.logout_url;
    } catch (error) {
      this.state.sessionError = error.message;
      this.render();
    }
  }
}

customElements.define("app-shell", AppShell);
