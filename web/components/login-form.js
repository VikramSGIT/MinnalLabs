import { postJSON } from "../lib/api.js";

class LoginForm extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this.apiBaseUrl = "";
    this.isSubmitting = false;
    this.mode = "login";
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

        .mode-toggle {
          display: inline-grid;
          grid-template-columns: repeat(2, minmax(0, 1fr));
          gap: 8px;
          margin-top: 20px;
          padding: 6px;
          border-radius: 16px;
          background: #eff6ff;
          border: 1px solid #dbeafe;
        }

        .mode-btn {
          border: 0;
          border-radius: 12px;
          padding: 0.8rem 1rem;
          font: inherit;
          font-weight: 700;
          color: #1d4ed8;
          background: transparent;
          cursor: pointer;
        }

        .mode-btn.active {
          color: #ffffff;
          background: #2563eb;
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

        .hint {
          min-height: 1.2rem;
          color: #0369a1;
          font-size: 0.95rem;
        }
      </style>
      <section class="panel">
        <h2>${this.mode === "login" ? "Login" : "Create User"}</h2>
        <p>
          ${
            this.mode === "login"
              ? "Use an existing backend account. The app relies on the backend's secure session cookie."
              : "Create a backend user account, then the app will sign you in automatically."
          }
        </p>
        <div class="mode-toggle" role="tablist" aria-label="Authentication Mode">
          <button
            id="loginModeBtn"
            type="button"
            class="mode-btn ${this.mode === "login" ? "active" : ""}"
            aria-selected="${this.mode === "login"}"
          >
            Sign In
          </button>
          <button
            id="signupModeBtn"
            type="button"
            class="mode-btn ${this.mode === "signup" ? "active" : ""}"
            aria-selected="${this.mode === "signup"}"
          >
            Create User
          </button>
        </div>
        <form id="loginForm">
          <label>
            Username
            <input id="username" name="username" type="text" autocomplete="username" required>
          </label>
          <label>
            Password
            <input id="password" name="password" type="password" autocomplete="${this.mode === "login" ? "current-password" : "new-password"}" required>
          </label>
          ${
            this.mode === "signup"
              ? `
                <label>
                  Confirm Password
                  <input id="confirmPassword" name="confirmPassword" type="password" autocomplete="new-password" required>
                </label>
              `
              : ""
          }
          <div id="hint" class="hint" role="status"></div>
          <div id="error" class="error" role="alert"></div>
          <button id="submitBtn" type="submit">${this.mode === "login" ? "Continue" : "Create User"}</button>
        </form>
      </section>
    `;

    this.form = this.shadowRoot.getElementById("loginForm");
    this.usernameInput = this.shadowRoot.getElementById("username");
    this.passwordInput = this.shadowRoot.getElementById("password");
    this.confirmPasswordInput = this.shadowRoot.getElementById("confirmPassword");
    this.submitBtn = this.shadowRoot.getElementById("submitBtn");
    this.hintEl = this.shadowRoot.getElementById("hint");
    this.errorEl = this.shadowRoot.getElementById("error");
    this.shadowRoot.getElementById("loginModeBtn").addEventListener("click", () => this.setMode("login"));
    this.shadowRoot.getElementById("signupModeBtn").addEventListener("click", () => this.setMode("signup"));
    this.form.addEventListener("submit", (event) => this.handleSubmit(event));
  }

  async handleSubmit(event) {
    event.preventDefault();
    if (this.isSubmitting) {
      return;
    }

    this.setError("");
    this.setHint("");
    this.setSubmitting(true);

    try {
      const response =
        this.mode === "signup"
          ? await this.createUserAndLogin()
          : await this.login({
              username: this.usernameInput.value.trim(),
              password: this.passwordInput.value,
            });

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

  async createUserAndLogin() {
    const username = this.usernameInput.value.trim();
    const password = this.passwordInput.value;
    const confirmPassword = this.confirmPasswordInput ? this.confirmPasswordInput.value : "";

    if (password !== confirmPassword) {
      throw new Error("Passwords do not match.");
    }

    await postJSON(
      "/api/enroll/user",
      {
        username,
        password,
      },
      this.apiBaseUrl,
    );

    this.setHint("User created. Signing you in...");

    try {
      return await this.login({ username, password });
    } catch (error) {
      this.mode = "login";
      this.render();
      this.usernameInput.value = username;
      this.setHint("User created. Please sign in to continue.");
      throw new Error("User created, but automatic sign-in failed. Please sign in.");
    }
  }

  async login(credentials) {
    return postJSON("/api/session/login", credentials, this.apiBaseUrl);
  }

  setMode(mode) {
    if (this.isSubmitting || this.mode === mode) {
      return;
    }

    this.mode = mode;
    this.render();
  }

  setSubmitting(isSubmitting) {
    this.isSubmitting = isSubmitting;
    const loginModeBtn = this.shadowRoot.getElementById("loginModeBtn");
    const signupModeBtn = this.shadowRoot.getElementById("signupModeBtn");
    loginModeBtn.disabled = isSubmitting;
    signupModeBtn.disabled = isSubmitting;
    this.submitBtn.disabled = isSubmitting;
    if (isSubmitting) {
      this.submitBtn.textContent = this.mode === "login" ? "Signing In..." : "Creating User...";
      return;
    }
    this.submitBtn.textContent = this.mode === "login" ? "Continue" : "Create User";
  }

  setError(message) {
    this.errorEl.textContent = message;
  }

  setHint(message) {
    this.hintEl.textContent = message;
  }
}

customElements.define("login-form", LoginForm);
