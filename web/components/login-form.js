import { postJSON } from "../lib/api.js";

class LoginForm extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.apiBaseUrl = "";
    this.isSubmitting = false;
  }

  connectedCallback() {
    this.render();
  }

  render() {
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

        form {
          margin-top: 24px;
          display: grid;
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
        <h2>Login</h2>
        <p>Use an existing backend user account. This lightweight frontend stores only the returned user ID in browser session storage.</p>
        <form id="loginForm">
          <label>
            Username
            <input id="username" name="username" type="text" autocomplete="username" required>
          </label>
          <label>
            Password
            <input id="password" name="password" type="password" autocomplete="current-password" required>
          </label>
          <div id="error" class="error" role="alert"></div>
          <button id="submitBtn" type="submit">Continue</button>
        </form>
      </section>
    `;

    this.form = this.shadowRoot.getElementById("loginForm");
    this.usernameInput = this.shadowRoot.getElementById("username");
    this.passwordInput = this.shadowRoot.getElementById("password");
    this.submitBtn = this.shadowRoot.getElementById("submitBtn");
    this.errorEl = this.shadowRoot.getElementById("error");
    this.form.addEventListener("submit", (event) => this.handleSubmit(event));
  }

  async handleSubmit(event) {
    event.preventDefault();
    if (this.isSubmitting) {
      return;
    }

    this.setError("");
    this.setSubmitting(true);

    try {
      const response = await postJSON(
        "/api/session/login",
        {
          username: this.usernameInput.value.trim(),
          password: this.passwordInput.value,
        },
        this.apiBaseUrl,
      );

      this.dispatchEvent(
        new CustomEvent("login-success", {
          bubbles: true,
          composed: true,
          detail: response,
        }),
      );
    } catch (error) {
      this.setError(error.message);
    } finally {
      this.setSubmitting(false);
    }
  }

  setSubmitting(isSubmitting) {
    this.isSubmitting = isSubmitting;
    this.submitBtn.disabled = isSubmitting;
    this.submitBtn.textContent = isSubmitting ? "Signing In..." : "Continue";
  }

  setError(message) {
    this.errorEl.textContent = message;
  }
}

customElements.define("login-form", LoginForm);
