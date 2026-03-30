# IoT Backend — Code Audit Report (2026-03-30)

**Date:** 2026-03-30
**Scope:** Full backend (Go), frontend (JS), migrations (SQL), infrastructure (Docker/Compose), device firmware config (YAML)

---

## Critical Bugs

### 1. Reflected XSS in the Login Page

**File:** `internal/oauth/oauth.go` lines 126–143

The `/login` GET handler interpolates the `redirect` query parameter directly into raw HTML without any escaping:

```go
redirect := c.Query("redirect")
html := `...
    <input type="hidden" name="redirect" value="` + redirect + `">
...`
```

An attacker can craft a URL such as `/login?redirect="><script>alert(document.cookie)</script>` to inject arbitrary JavaScript and steal session cookies.

**Recommendation:** HTML-escape the `redirect` value before interpolating, or use Go's `html/template` package.

---

### 2. Open Redirect After Login

**File:** `internal/oauth/oauth.go` lines 146–161

After a successful POST to `/login`, the `redirect` form value is passed directly to `c.Redirect()` with no validation:

```go
redirect := c.PostForm("redirect")
c.Redirect(http.StatusFound, redirect)
```

An attacker can set `redirect` to an external phishing domain. A user who enters valid credentials is silently sent to the attacker's site.

**Recommendation:** Validate that `redirect` is a relative path or matches a known allowlist of origins.

---

### 3. DELETE Method Missing from CORS Whitelist

**File:** `cmd/server/main.go` lines 36–42

```go
AllowMethods: []string{"GET", "POST", "OPTIONS"},
```

The frontend uses `DELETE` for home and device deletion (`home-create-form.js`, `home-device-manager.js`). In the Docker Compose setup (frontend on `:3000`, backend on `:8080`), the browser's CORS preflight will reject these cross-origin DELETE requests.

**Recommendation:** Add `"DELETE"` (and `"PUT"`, `"PATCH"` for future-proofing) to `AllowMethods`.

---

### 4. Device Cache Not Rebuilt from Database on Startup

**Files:** `internal/state/state.go`, `cmd/server/main.go`

On startup, `SyncProductCaps()` loads product capabilities into Valkey, but there is no equivalent function to load existing devices from PostgreSQL into the Valkey device set. Devices are only cached via `CacheDevice()` during enrollment.

If Valkey data is lost (volume corruption, accidental flush, cold start without persistent volume), the system enters a broken state:

- `connectHandler` in `mqtt.go` calls `state.GetAllDevices()` which returns nothing — no MQTT subscriptions are created for existing devices.
- Google Home SYNC returns an empty device list.
- Device presence and state tracking is silently broken.

Valkey is used as a **primary data store** for device metadata, not as a rebuildable cache. There is no recovery path short of re-enrolling every device.

**Recommendation:** Add a `SyncDevices()` function that loads all device metadata from PostgreSQL into Valkey on startup, similar to `SyncProductCaps()`.

---

### 5. Google Fulfillment Endpoint Has No Authentication

**File:** `internal/google/google.go` lines 31–66

```go
r.POST("/api/google/fulfillment", func(c *gin.Context) {
    // No auth middleware, no OAuth token validation
```

The fulfillment endpoint accepts unauthenticated requests. Anyone who discovers the URL can query the state of all devices and send commands to any device. In a proper Google Smart Home integration, this endpoint must validate the OAuth bearer token in the `Authorization` header.

**Recommendation:** Add middleware that extracts and validates the OAuth access token using the `oauth.Srv` server instance.

---

### 6. Hardcoded `agentUserId` Breaks Multi-User

**File:** `internal/google/google.go` lines 119–123

```go
return map[string]interface{}{
    "agentUserId": "user123",
    "devices":     googleDevices,
}
```

All users' devices are returned under a single hardcoded `agentUserId`. Google Home sees every user's devices as belonging to one account, making multi-tenancy fundamentally broken.

**Recommendation:** Derive `agentUserId` from the authenticated user's ID (extracted from the OAuth token on the fulfillment request).

---

## Security Concerns

### 7. User Registration Is Fully Open

**File:** `internal/enrollment/enrollment.go` line 52

```go
api.POST("/user", enrollUser)
```

`enrollUser` has no authentication guard and no rate limiting. Anyone can create unlimited accounts.

**Recommendation:** Require an admin invite token, or gate registration behind an admin-only endpoint. At minimum, add rate limiting.

---

### 8. Hardcoded OAuth Client Secret

**File:** `internal/oauth/oauth.go` lines 48–54, `migrations/001_init.sql` line 102–103

Two different default OAuth clients are seeded with the same weak secret `my-secret-key`:

- Migration seeds client ID `google-client`
- Go code creates client ID `google-alexa-client` if no clients exist

**Recommendation:** Remove hardcoded secrets. Generate a random secret on first run and log it once, or require it as an environment variable.

---

### 9. Internal Error Messages Leaked to Clients

**Files:** `internal/enrollment/enrollment.go` lines 404, 541

```go
c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
```

Raw error messages from database operations, MQTT broker interactions, and internal logic are returned to the client. These can leak database schema details, MQTT broker hostnames, or internal file paths.

**Recommendation:** Return generic error messages to clients and log the detailed error server-side.

---

### 10. WiFi and MQTT Credentials Exposed in Enrollment Response

**File:** `internal/enrollment/enrollment.go` lines 612–624

```go
c.JSON(http.StatusCreated, gin.H{
    "mqtt_password": home.MQTTPassword,
    "wifi_password": home.WiFiPassword,
    // ...
})
```

Plaintext WiFi and MQTT passwords are returned in the HTTP response body. While the frontend needs them for BLE provisioning, any MitM or XSS attack would expose these credentials.

**Recommendation:** Consider a one-time token mechanism or end-to-end encryption for credential delivery. At minimum, ensure the frontend connection uses HTTPS.

---

## Design Issues

### 11. In-Memory OAuth Token Store Loses All Tokens on Restart

**File:** `internal/oauth/oauth.go` line 29

```go
manager.MustTokenStorage(store.NewMemoryTokenStore())
```

Every server restart invalidates all OAuth tokens. Google Home loses authorization and stops working until the user re-links their account.

**Recommendation:** Use a persistent token store backed by PostgreSQL or Valkey (e.g., `go-oauth2-gorm` or a Redis-backed store).

---

### 12. Migration System Has No Idempotency Tracking

**File:** `internal/db/db.go` lines 36–63

All SQL migration files are executed on every startup. This works only because of `IF NOT EXISTS` / `ON CONFLICT` guards, but it is fragile. Any future migration that performs data manipulation (UPDATE, INSERT without ON CONFLICT) will execute repeatedly.

**Recommendation:** Add a `schema_migrations` table that tracks which migrations have been applied, and skip already-applied ones.

---

### 13. MQTT Cleanup Inside Database Transaction

**File:** `internal/enrollment/enrollment.go` lines 380–406

MQTT dynsec commands (network calls with timeouts) are executed inside a GORM database transaction. If the MQTT command partially succeeds (e.g., deletes the client but fails on role deletion), the DB transaction rolls back but the MQTT state is already modified — leaving the system inconsistent.

**Recommendation:** Move MQTT cleanup outside the transaction, or implement a compensating transaction pattern.

---

### 14. Race Condition on `statusUpdateHook`

**File:** `internal/mqtt/mqtt.go` lines 15–19

```go
var statusUpdateHook func(deviceID uint, status string)
```

This global function pointer is written during startup (`RegisterStatusUpdateHook`) and read from the MQTT callback goroutine without synchronization. While in practice it is set once before MQTT connects, it is technically a data race.

**Recommendation:** Use `sync.Once`, `atomic.Value`, or pass the hook via a struct field.

---

### 15. N+1 Query in Rollout Summaries

**File:** `internal/ota/rollout.go` lines 299–304

For each rollout, **six separate COUNT queries** are issued to count device states. With 20 rollouts (the default limit), that is 120 queries per request.

**Recommendation:** Replace with a single aggregated query:

```sql
SELECT rollout_id, state, COUNT(*) FROM firmware_rollout_devices
WHERE rollout_id IN (?) GROUP BY rollout_id, state
```

---

### 16. N+1 Query in Google SYNC

**File:** `internal/google/google.go` lines 86–93

One DB query per device to fetch the device name.

**Recommendation:** Batch-fetch all device names in a single `WHERE id IN (...)` query.

---

### 17. Session TTL Defined in Two Places

**Files:** `internal/config/config.go` line 158–160, `internal/state/session.go` line 12

The session TTL is independently defined as `7 * 24 * time.Hour` in both locations. If one is changed, the other silently remains different, leading to confusing session expiration behavior.

**Recommendation:** Use a single source of truth. Have `state/session.go` accept the TTL as a parameter or read it from the config.

---

### 18. `oauth_tokens` Table Exists but Is Unused

**Files:** `internal/models/models.go` lines 122–132, `migrations/001_init.sql` lines 68–76

The `OAuthToken` model and `oauth_tokens` database table are defined but never read from or written to. The OAuth system uses an in-memory token store instead.

**Recommendation:** Remove the dead model and table, or implement a persistent token store that uses them.

---

### 19. Redundant User Lookup in `enrollHome`

**File:** `internal/enrollment/enrollment.go` lines 485–489

```go
var user models.User
if err := db.DB.First(&user, sessionUser.UserID).Error; err != nil {
    c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
    return
}
```

The user is already authenticated via the session middleware. The fetched `user` variable is never used after this existence check. This is a wasted database query.

**Recommendation:** Remove the lookup; the session middleware already guarantees the user exists.

---

### 20. No Graceful Shutdown

**File:** `cmd/server/main.go` lines 51–53

```go
if err := r.Run(serverAddr); err != nil {
    log.Fatalf("Failed to start server: %v", err)
}
```

`r.Run()` does not handle OS signals. On `docker stop` (SIGTERM), in-flight HTTP requests, MQTT publishes, and database transactions are terminated mid-operation.

**Recommendation:** Use `http.Server` with signal handling and `server.Shutdown(ctx)` for graceful draining.

---

### 21. Global Package-Level Mutable State

Packages rely heavily on global mutable variables initialized via init-style functions:

| Package | Variable |
|---|---|
| `db` | `DB` |
| `mqtt` | `Client` |
| `state` | `rdb`, `gdb`, `ctx` |
| `oauth` | `Srv`, `appCfg` |
| `admin` | `cfg` |
| `enrollment` | `cfg` |

This makes unit testing impossible without global state mutation, keeps dependency relationships implicit, and prevents safe concurrent test execution.

**Recommendation:** Refactor to dependency injection using structs that hold dependencies.

---

### 22. No Database Connection Pool Tuning

**File:** `internal/db/db.go` line 28

```go
DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
```

Uses GORM defaults for pool size, idle connections, and lifetime. Under load (particularly from the N+1 queries and the 30-second OTA worker loop), this could exhaust connections or leave them stale.

**Recommendation:** Configure `SetMaxOpenConns`, `SetMaxIdleConns`, and `SetConnMaxLifetime` on the underlying `*sql.DB`.

---

## Code Duplication

### 23. `escapeHtml` Copy-Pasted in 5 Frontend Files

Identical implementations exist in:

- `web/components/app-shell.js`
- `web/components/device-enrollment.js`
- `web/components/home-create-form.js`
- `web/components/home-device-manager.js`
- `web/components/admin-firmware-manager.js`

**Recommendation:** Extract to a shared utility module (e.g., `web/lib/html.js`).

---

### 24. `formatTimestamp` Copy-Pasted in 3 Frontend Files

Duplicated across `device-enrollment.js`, `home-device-manager.js`, and `admin-firmware-manager.js` (with minor wording differences in the fallback text).

**Recommendation:** Extract to a shared utility module.

---

### 25. `effectiveRolloutPercentage` / `effectiveRolloutIntervalMinutes` Duplicated in Backend

These functions have identical implementations in both `internal/admin/admin.go` and `internal/ota/rollout.go`.

**Recommendation:** Define once (e.g., as methods on `models.Product`) and import from a single location.

---

## Minor Issues

### 26. Two Different Default OAuth Clients

Migration `001_init.sql` seeds client ID `google-client`, while `oauth.go` creates `google-alexa-client` if no clients exist. After the migration runs and seeds `google-client`, the Go code's `len(dbClients) == 0` check is false, so `google-alexa-client` is never created. The inconsistent naming is confusing.

**Recommendation:** Use a single, consistent default client definition.

---

### 27. Valkey Context Is Non-Cancellable

**File:** `internal/state/state.go` line 21

```go
ctx = context.Background()
```

A package-level `context.Background()` is shared by all Redis operations. No operation can be individually timed out or cancelled, which can cause goroutine hangs if Valkey becomes unresponsive.

**Recommendation:** Create per-operation contexts with timeouts (e.g., `context.WithTimeout`).

---

## Summary

| Severity | Count | Key Items |
|---|---|---|
| **Critical Bugs** | 6 | XSS, open redirect, CORS blocks DELETE, device cache loss, unauthenticated fulfillment, broken multi-user |
| **Security** | 4 | Open registration, hardcoded secrets, error leaks, credential exposure |
| **Design Flaws** | 12 | Token loss on restart, no migration tracking, N+1 queries, race conditions, no graceful shutdown |
| **Code Quality** | 5 | Duplicated functions, dead code, unnecessary queries |

### Recommended Priority Order

1. **XSS and open redirect** (#1, #2) — immediate exploitation risk
2. **CORS DELETE fix** (#3) — frontend functionality is broken in production
3. **Unauthenticated fulfillment** (#5) — anyone can control all devices
4. **Device cache rebuild** (#4) — system breaks silently after Valkey data loss
5. **Hardcoded `agentUserId`** (#6) — multi-user is fundamentally broken
6. **Graceful shutdown and connection pool tuning** (#20, #22) — production stability
7. **Remaining security and design items** — iterative improvement
