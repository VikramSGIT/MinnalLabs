import { escapeHtml } from "../lib/html.js";

const FRIENDLY_LABELS = {
  "traits.username": "Username",
  "traits.email": "Email",
  "traits.name": "Name",
  password: "Password",
  identifier: "Username or Email",
  "password_identifier": "Username or Email",
  code: "Verification Code",
};

function friendlyLabel(node) {
  const name = (node.attributes && node.attributes.name) || "";
  if (FRIENDLY_LABELS[name]) return FRIENDLY_LABELS[name];
  const meta = node.meta && node.meta.label && node.meta.label.text;
  return meta || name;
}

class KratosFlow extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.flow = null;
    this.error = "";
    this.isLoading = true;
  }

  get flowType() {
    return this.getAttribute("flow-type") || "login";
  }

  connectedCallback() {
    this.render();
    this.initFlow();
  }

  async initFlow() {
    const params = new URLSearchParams(window.location.search);
    const flowId = params.get("flow");

    if (!flowId) {
      const returnTo = params.get("return_to");
      const initUrl =
        "/self-service/" +
        this.flowType +
        "/browser" +
        (returnTo ? "?return_to=" + encodeURIComponent(returnTo) : "");
      window.location.href = initUrl;
      return;
    }

    await this.fetchFlow(flowId);
  }

  async fetchFlow(flowId) {
    this.isLoading = true;
    this.error = "";
    this.render();

    try {
      const resp = await fetch(
        "/self-service/" + this.flowType + "/flows?id=" + encodeURIComponent(flowId),
        {
          headers: { Accept: "application/json" },
          credentials: "include",
        },
      );

      if (resp.status === 410) {
        this.error = "This flow has expired.";
        this.flow = null;
        this.isLoading = false;
        this.render();
        return;
      }

      if (!resp.ok) {
        throw new Error("Failed to load flow (HTTP " + resp.status + ")");
      }

      this.flow = await resp.json();
    } catch (err) {
      this.error = err.message;
      this.flow = null;
    } finally {
      this.isLoading = false;
      this.render();
    }
  }

  async handleSubmit(event, submitName, submitValue) {
    event.preventDefault();
    if (!this.flow) return;

    const form = this.shadowRoot.querySelector("form");
    if (!form) return;

    // Registration: validate confirm password and check availability
    if (this.flowType === "registration" && (!submitName || submitName !== "provider")) {
      const password = form.querySelector('input[name="password"]');
      const confirmPassword = this.shadowRoot.getElementById("confirm_password");
      if (password && confirmPassword && password.value !== confirmPassword.value) {
        this.error = "Passwords do not match.";
        this.render();
        return;
      }

      const username = form.querySelector('input[name="traits.username"]');
      const email = form.querySelector('input[name="traits.email"]');
      const params = new URLSearchParams();
      if (username && username.value) params.set("username", username.value);
      if (email && email.value) params.set("email", email.value);

      if (params.toString()) {
        try {
          const checkResp = await fetch("/api/auth/check-availability?" + params.toString());
          const checkData = await checkResp.json();
          const errors = [];
          if (checkData.username_available === false) errors.push("Username is already taken.");
          if (checkData.email_available === false) errors.push("Email is already registered.");
          if (errors.length > 0) {
            this.error = errors.join(" ");
            this.render();
            return;
          }
        } catch (_) {
          // If check fails, let Kratos handle validation
        }
      }
    }

    const formData = new URLSearchParams(new FormData(form));
    // Remove confirm_password — not a Kratos field
    formData.delete("confirm_password");
    if (submitName) {
      formData.set(submitName, submitValue || "");
    }

    try {
      const resp = await fetch(this.flow.ui.action, {
        method: this.flow.ui.method || "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          Accept: "application/json",
        },
        credentials: "include",
        body: formData.toString(),
      });

      const data = await resp.json();

      // 422 or any response with redirect_browser_to — follow it (verification UI, OIDC, etc.)
      if (data.redirect_browser_to) {
        window.location.href = data.redirect_browser_to;
        return;
      }

      // Check continue_with for verification flow redirect
      if (data.continue_with && data.continue_with.length > 0) {
        for (const action of data.continue_with) {
          if (action.action === "show_verification_ui" && action.flow && action.flow.url) {
            window.location.href = action.flow.url;
            return;
          }
        }
      }

      // 400 with updated flow — validation errors (duplicates, invalid input, etc.)
      if (data.ui) {
        this.flow = data;
        this.error = "";
        this.render();
        return;
      }

      // 200 success with no redirect — go home
      if (resp.ok) {
        window.location.href = "/";
        return;
      }

      // Error object from Kratos
      this.error = (data.error && data.error.message) || data.message || "Something went wrong.";
      this.render();
    } catch (err) {
      this.error = err.message;
      this.render();
    }
  }

  render() {
    const title = {
      login: "Sign In",
      registration: "Create Account",
      verification: "Verify Email",
      settings: "Account Settings",
    }[this.flowType] || "Authentication";

    this.shadowRoot.innerHTML = `
      <style>
        :host { display: block; }
        *, *::before, *::after { box-sizing: border-box; }

        .panel {
          padding: 28px;
          border-radius: 24px;
          border: 1px solid #dbeafe;
          background: #ffffff;
          box-shadow: 0 20px 44px rgba(15, 23, 42, 0.08);
        }

        h2 { margin: 0; font-size: 1.5rem; }
        p { margin: 12px 0 0; color: #475569; line-height: 1.6; }

        form { margin-top: 24px; display: grid; gap: 18px; }

        label {
          display: grid;
          gap: 8px;
          font-weight: 600;
          color: #0f172a;
        }

        input:not([type="hidden"]):not([type="submit"]) {
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

        button, input[type="submit"] {
          border: 0;
          border-radius: 14px;
          padding: 0.95rem 1.2rem;
          font: inherit;
          font-weight: 700;
          color: #ffffff;
          background: #2563eb;
          cursor: pointer;
          width: 100%;
        }

        button:disabled { opacity: 0.65; cursor: wait; }

        .oidc-btn {
          display: flex;
          align-items: center;
          justify-content: center;
          gap: 10px;
          width: 100%;
          padding: 0.9rem 1rem;
          border: 1px solid #cbd5e1;
          border-radius: 14px;
          background: #ffffff;
          color: #0f172a;
          font: inherit;
          font-weight: 600;
          cursor: pointer;
        }

        .oidc-btn:hover {
          background: #f8fafc;
          border-color: #94a3b8;
        }

        .divider {
          display: flex;
          align-items: center;
          gap: 12px;
          margin: 4px 0;
          color: #94a3b8;
          font-size: 0.85rem;
          text-transform: uppercase;
          letter-spacing: 0.06em;
        }

        .divider::before, .divider::after {
          content: "";
          flex: 1;
          height: 1px;
          background: #e2e8f0;
        }

        .msg-error {
          color: #b91c1c;
          font-size: 0.9rem;
          margin: 2px 0 0;
        }

        .msg-info {
          color: #0369a1;
          font-size: 0.9rem;
          margin: 2px 0 0;
        }

        .msg-success {
          color: #15803d;
          font-size: 0.9rem;
          margin: 2px 0 0;
        }

        .banner-error {
          padding: 14px 18px;
          border-radius: 16px;
          border: 1px solid #fecaca;
          background: #fef2f2;
          color: #b91c1c;
          font-weight: 600;
          margin-bottom: 16px;
        }

        .banner-info {
          padding: 14px 18px;
          border-radius: 16px;
          border: 1px solid #bfdbfe;
          background: #eff6ff;
          color: #1d4ed8;
          font-weight: 600;
          margin-bottom: 16px;
        }

        .footer {
          margin-top: 20px;
          text-align: center;
          color: #475569;
          font-size: 0.95rem;
        }

        .footer a {
          color: #2563eb;
          font-weight: 600;
          text-decoration: none;
        }

        .footer a:hover { text-decoration: underline; }

        .group-section { margin-top: 8px; display: grid; gap: 14px; }
      </style>
      <section class="panel">
        <h2>${escapeHtml(title)}</h2>
        ${this.renderBody()}
      </section>
    `;

    this.bindEvents();
  }

  renderBody() {
    if (this.isLoading) {
      return "<p>Loading...</p>";
    }

    if (this.error && !this.flow) {
      return `
        <div class="banner-error">${escapeHtml(this.error)}</div>
        <p><a href="/self-service/${this.flowType}/browser">Try again</a></p>
      `;
    }

    if (!this.flow || !this.flow.ui) {
      return "<p>Unable to load authentication flow.</p>";
    }

    const ui = this.flow.ui;
    let html = "";

    // Global messages
    if (ui.messages && ui.messages.length > 0) {
      for (const msg of ui.messages) {
        const cls = msg.type === "error" ? "banner-error" : "banner-info";
        html += '<div class="' + cls + '">' + escapeHtml(msg.text) + "</div>";
      }
    }

    // Group nodes
    const groups = {};
    for (const node of ui.nodes) {
      const g = node.group || "default";
      if (!groups[g]) groups[g] = [];
      groups[g].push(node);
    }

    html += '<form action="' + escapeHtml(ui.action) + '" method="' + escapeHtml(ui.method || "POST") + '">';

    // Default group (csrf, identifier)
    if (groups.default) {
      html += '<div class="group-section">';
      html += this.renderNodes(groups.default);
      html += "</div>";
    }

    // Password group
    if (groups.password) {
      html += '<div class="group-section">';
      html += this.renderNodes(groups.password);
      // Add confirm password for registration
      if (this.flowType === "registration") {
        html +=
          "<label>Confirm Password" +
          '<input type="password" id="confirm_password" name="confirm_password" autocomplete="new-password" required>' +
          "</label>";
      }
      html += "</div>";
    }

    // Code group (verification)
    if (groups.code) {
      html += '<div class="group-section">';
      html += this.renderNodes(groups.code);
      html += "</div>";
    }

    // Profile group (settings)
    if (groups.profile) {
      html += '<div class="group-section">';
      html += this.renderNodes(groups.profile);
      html += "</div>";
    }

    // OIDC group
    if (groups.oidc) {
      html += '<div class="divider">or</div>';
      html += '<div class="group-section">';
      html += this.renderNodes(groups.oidc);
      html += "</div>";
    }

    html += "</form>";

    // Footer links
    if (this.flowType === "login") {
      html +=
        '<div class="footer">Don\'t have an account? <a href="/self-service/registration/browser">Sign up</a></div>';
    } else if (this.flowType === "registration") {
      html +=
        '<div class="footer">Already have an account? <a href="/self-service/login/browser">Sign in</a></div>';
    }

    return html;
  }

  renderNodes(nodes) {
    let html = "";
    for (const node of nodes) {
      html += this.renderNode(node);
    }
    return html;
  }

  renderNode(node) {
    const attr = node.attributes || {};
    const messages = node.messages || [];

    if (node.type === "input") {
      if (attr.type === "hidden") {
        return (
          '<input type="hidden" name="' +
          escapeHtml(attr.name || "") +
          '" value="' +
          escapeHtml(attr.value || "") +
          '">'
        );
      }

      if (attr.type === "submit") {
        const label = (node.meta && node.meta.label && node.meta.label.text) || attr.value || "Submit";
        // Submit buttons use Kratos label directly (e.g. "Sign up", "Sign in")

        // OIDC provider buttons
        if (node.group === "oidc") {
          return (
            '<button type="submit" class="oidc-btn" name="' +
            escapeHtml(attr.name || "") +
            '" value="' +
            escapeHtml(attr.value || "") +
            '" data-submit="true">' +
            this.providerIcon(attr.value) +
            escapeHtml(label) +
            "</button>"
          );
        }

        return (
          '<button type="submit" name="' +
          escapeHtml(attr.name || "") +
          '" value="' +
          escapeHtml(attr.value || "") +
          '" data-submit="true">' +
          escapeHtml(label) +
          "</button>"
        );
      }

      // Regular input
      const label = friendlyLabel(node);
      return (
        "<label>" +
        escapeHtml(label) +
        '<input type="' +
        escapeHtml(attr.type || "text") +
        '" name="' +
        escapeHtml(attr.name || "") +
        '" value="' +
        escapeHtml(attr.value || "") +
        '"' +
        (attr.required ? " required" : "") +
        (attr.disabled ? " disabled" : "") +
        (attr.autocomplete ? ' autocomplete="' + escapeHtml(attr.autocomplete) + '"' : "") +
        (attr.pattern ? ' pattern="' + escapeHtml(attr.pattern) + '"' : "") +
        ">" +
        this.renderMessages(messages) +
        "</label>"
      );
    }

    if (node.type === "text") {
      const text = (node.attributes && node.attributes.text && node.attributes.text.text) || "";
      return "<p>" + escapeHtml(text) + "</p>" + this.renderMessages(messages);
    }

    if (node.type === "img") {
      const src = (attr && attr.src) || "";
      return '<img src="' + escapeHtml(src) + '" alt="">';
    }

    return "";
  }

  renderMessages(messages) {
    if (!messages || messages.length === 0) return "";
    let html = "";
    for (const msg of messages) {
      const cls = msg.type === "error" ? "msg-error" : msg.type === "success" ? "msg-success" : "msg-info";
      html += '<div class="' + cls + '">' + escapeHtml(msg.text) + "</div>";
    }
    return html;
  }

  providerIcon(provider) {
    if (provider === "google") {
      return `<svg viewBox="0 0 24 24" width="20" height="20" style="flex:0 0 auto"><path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/><path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/><path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/><path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/></svg>`;
    }
    return "";
  }

  bindEvents() {
    const form = this.shadowRoot.querySelector("form");
    if (!form) return;

    const submitButtons = this.shadowRoot.querySelectorAll("[data-submit]");
    for (const btn of submitButtons) {
      btn.addEventListener("click", (e) => {
        e.preventDefault();
        this.handleSubmit(e, btn.getAttribute("name"), btn.getAttribute("value"));
      });
    }
  }
}

customElements.define("kratos-flow", KratosFlow);
