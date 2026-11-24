# Pending

## Home Assistant

Home Assistant runs on the same Pi (rpi-homeserver, host network, `https://home.<domain>`). Two
steps, the first needing no code here.

### 1. Home Assistant sends alerts to pixelhub

A `rest_command` in Home Assistant calling `POST /api/notify`, used from its automations:

- the robot vacuum's countdown before it cleans the office ("5 min"), next to the phone warning;
- robot stuck, out of water, cleaning finished;
- any other rule worth a glance at the desk.

Open questions: which address Home Assistant reaches pixelhub on (it is on the host network,
pixelhub on `media-network`: through Caddy or a loopback port), the alert `source` and which
channels accept it, and whether `/api/notify` needs a token once something other than the web UI
calls it.

### 2. pixelhub as a Home Assistant device (MQTT Discovery)

pixelhub announces itself with MQTT Discovery, the way ESPHome and Zigbee2MQTT do, so Home Assistant
gets a "Divoom" device without any YAML:

- `select`: active channel; `light` or `number`: brightness; `switch`: display on/off;
- `sensor`: what is on screen, display battery, bridge connected; `binary_sensor`: charging;
- `notify` or a `text` entity for alerts, replacing step 1's `rest_command`.

Needs an MQTT broker (Mosquitto as its own service in rpi-homeserver) and a small MQTT client here.
State is published on change and commands are subscribed to; nothing replaces the HTTP API.

Not `d03n3rfr1tz3/hass-divoom`: it drives the same esp32-divoom bridge, which takes one connection,
so it would fight pixelhub for the display, and it covers single commands only.
