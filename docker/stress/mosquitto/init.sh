#!/bin/sh

set -eu

data_dir="/mosquitto/data"
config_file="${data_dir}/dynamic-security.json"
admin_username="${MQTT_ADMIN_USERNAME:-admin}"
admin_password="${MQTT_ADMIN_PASSWORD:-admin-password}"

mkdir -p "${data_dir}"

if [ ! -f "${config_file}" ]; then
  echo "Initializing Mosquitto dynamic security configuration"
  mosquitto_ctrl dynsec init "${config_file}" "${admin_username}" "${admin_password}"
fi

chown -R 1883:1883 "${data_dir}"
