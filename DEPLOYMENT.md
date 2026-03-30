# Deployment Guide

This guide explains how to deploy the Go IoT Backend with a secure Cloudflare Tunnel to expose your server to Google Assistant without port forwarding on your router.

## Prerequisites

1. Docker and Docker Compose installed on your host machine (e.g., a Raspberry Pi).
2. A Cloudflare account and a domain name managed by Cloudflare.

## Setup Cloudflare Tunnel

To securely connect Google to your local server:

1. Log in to your Cloudflare Dashboard.
2. Go to **Zero Trust** -> **Networks** -> **Tunnels**.
3. Click **Create a tunnel**.
4. Choose **Cloudflared** as the connector type.
5. Name your tunnel (e.g., `iot-backend`).
6. Copy the generated **token** string from the install command. It looks something like `eyJh...`.
7. Save this token as `CLOUDFLARE_TUNNEL_TOKEN` in a `.env` file in the root of your project:
   ```env
   CLOUDFLARE_TUNNEL_TOKEN=your_copied_token_here
   DB_USER=iotuser
   DB_PASSWORD=iotpassword
   DB_NAME=iotdb
   ```
8. In the Cloudflare Zero Trust dashboard, set up a **Public Hostname** (e.g., `iot.yourdomain.com`) and point it to the local service:
   * Service Type: `HTTP`
   * URL: `backend:8080` (this resolves to the `backend` container inside the Docker network).

## MQTT Broker Setup

This backend assumes your MQTT broker is an external Mosquitto instance and that
the backend connects using a Dynamic Security admin client. The backend uses
that admin connection to create one MQTT username/password per home and to add
ACLs limited to that home's topic prefix.

### Required broker capabilities

1. Mosquitto Dynamic Security plugin enabled.
2. A backend admin MQTT client with permission to manage `$CONTROL/dynamic-security/#`.
3. Backend environment variables set with that admin client's credentials:
   ```env
   MQTT_BROKER=tcp://your-broker-host:1883
   MQTT_USERNAME=backend-admin
   MQTT_PASSWORD=backend-admin-password
   MQTT_HOST=your-broker-host
   MQTT_PORT=1883
   ```

### Example Dynamic Security bootstrap

Create an admin user and grant it the built-in `super-admin` role. With
`mosquitto_ctrl`, the typical setup is:

```bash
mosquitto_ctrl dynsec init /path/to/dynamic-security.json admin admin-password
```

Then use that admin username/password as the backend's `MQTT_USERNAME` and
`MQTT_PASSWORD`.

### Home ACL behavior

When the frontend calls `POST /api/enroll/home`, the backend will:

1. Generate a unique MQTT username/password for the home.
2. Create a Dynamic Security role named from the user/home IDs.
3. Allow that role to access only the home's topic subtree:
   ```text
   {user_id}/{home_id}/#
   ```
4. Create the MQTT client and bind the role to it.

Device enrollment then reuses the stored home MQTT credentials.

### Device online/offline status

Device presence is tracked with retained MQTT LWT status messages on:

```text
{user_id}/{home_id}/{device_id}/status
```

Each device publishes `online_v1.0.1` (including its current firmware version)
as its retained birth message and `offline` as its retained will/shutdown
message. The backend subscribes to those status topics and mirrors the latest
value into Valkey while also persisting the latest reported firmware version on
the device row in PostgreSQL. This lets the enrollment APIs keep returning the
last known firmware version even after a backend restart or empty Valkey cache.

### Firmware rollout channels

Firmware rollout messages are published per device:

```text
{user_id}/{home_id}/{device_id}/firmware_update
```

The backend rollout worker sends these OTA commands as **retained** JSON
messages containing `version`, `url`, and `md5_url`:

1. Each batch publishes one retained OTA command per selected device.
2. If a device is offline, it receives the retained command when it reconnects.
3. After the device reports the target firmware version in `online_vX`, the
   backend clears the retained OTA command for that device.

This allows a staged rollout without losing update commands for temporarily
offline devices.

## Firmware Hosting With Caddy

The Docker stack exposes firmware files through a dedicated Caddy service on
port `8081`. Firmware files are stored in `./firmware` and served under:

```text
http://<host>:8081/firmware/<product_id>_<version>.bin
```

Example:

```text
http://localhost:8081/firmware/1_v1.0.2.bin
```

Each uploaded binary also gets a companion MD5 text file served from the same
location with a `.md5` suffix, for example:

```text
http://localhost:8081/firmware/1_v1.0.2.bin.md5
```

The backend reads `firmware_url` and `firmware_md5_url` from the `products`
table when publishing OTA commands. Those values can be provisioned by another
backend or deployment workflow. The stored value may be either a root/base URL
or an existing artifact URL; this service derives the version-specific
`<product_id>_<version>.bin` and `.bin.md5` paths from the current firmware
version when building OTA links.

## Admin Users

Admin access is controlled by the `admin_users` table, not a flag on `users`.
To promote a user manually:

```sql
INSERT INTO admin_users (user_id) VALUES (<user_id>);
```

Admin users can:

- upload firmware binaries for products
- update the product's current firmware metadata in PostgreSQL
- create percentage-based staged rollout jobs
- choose the wait interval between batches in hours or days

## Run the Application

Run the stack with:

```bash
docker-compose up -d
```

## Cloud Integrations

### Google Assistant
1. Go to the [Actions on Google Console](https://console.actions.google.com/).
2. Create a new Smart Home project.
3. Set the **Fulfillment URL** to `https://iot.yourdomain.com/api/google/fulfillment`.
4. Set up **Account Linking**:
   * Client ID: `google-client` (or the value of `OAUTH_CLIENT_ID` if overridden)
   * Client Secret: use the value configured in `OAUTH_CLIENT_SECRET`
   * Authorization URL: `https://iot.yourdomain.com/oauth/authorize`
   * Token URL: `https://iot.yourdomain.com/oauth/token`
