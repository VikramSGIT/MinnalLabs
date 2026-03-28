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

## Run the Application

1. Create necessary Mosquitto config files:
   ```bash
   mkdir -p mosquitto/config mosquitto/data mosquitto/log
   ```
2. Create a basic `mosquitto/config/mosquitto.conf`:
   ```conf
   listener 1883
   allow_anonymous true
   ```
3. Run `docker-compose up -d`.

## Cloud Integrations

### Google Assistant
1. Go to the [Actions on Google Console](https://console.actions.google.com/).
2. Create a new Smart Home project.
3. Set the **Fulfillment URL** to `https://iot.yourdomain.com/api/google/fulfillment`.
4. Set up **Account Linking**:
   * Client ID: `google-alexa-client`
   * Client Secret: `my-secret-key` (from `oauth.go` defaults)
   * Authorization URL: `https://iot.yourdomain.com/oauth/authorize`
   * Token URL: `https://iot.yourdomain.com/oauth/token`
