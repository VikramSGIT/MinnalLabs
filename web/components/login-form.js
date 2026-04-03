import { postJSON } from "../lib/api.js";
import { getFrontendConfig } from "../lib/config.js";

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

        .divider {
          display: flex;
          align-items: center;
          gap: 12px;
          margin: 20px 0 4px;
          color: #94a3b8;
          font-size: 0.85rem;
          text-transform: uppercase;
          letter-spacing: 0.06em;
        }

        .divider::before,
        .divider::after {
          content: "";
          flex: 1;
          height: 1px;
          background: #e2e8f0;
        }

        .google-btn {
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
          text-decoration: none;
        }

        .google-btn:hover {
          background: #f8fafc;
          border-color: #94a3b8;
        }

        .google-btn svg {
          width: 20px;
          height: 20px;
          flex: 0 0 auto;
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
        <div class="divider">or</div>
        <a id="googleBtn" class="google-btn" href="#">
          <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
            <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/>
            <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
            <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/>
            <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
          </svg>
          Sign in with Google
        </a>
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

    const googleBtn = this.shadowRoot.getElementById("googleBtn");
    if (googleBtn) {
      const base = (this.apiBaseUrl || getFrontendConfig().apiBaseUrl).replace(/\/$/, "");
      googleBtn.href = base + "/auth/google/login";
    }
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
