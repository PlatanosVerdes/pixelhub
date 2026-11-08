<h1 align="center">pixelhub</h1>

Drives a Divoom Timebox Evo, a 16x16 pixel display that only speaks Bluetooth, through an ESP32
running the esp32-divoom bridge firmware, which turns TCP into Bluetooth. pixelhub is a JSON API
under `/api`, and it also serves a [web UI](#web-ui) at `/` that uses only that public API.

The display runs one **channel** at a time. The active one is a manual override from
`POST /api/channel` if there is one, else the channel schedule, else the default channel. Each
channel has its own scenes, ambient pictures and accepted alert sources. What is on screen comes
from three layers, highest first:

1. **Alerts** from `POST /api/notify`, queued by priority, each for a while (10 s by default). A
   channel lists the `source`s it shows (`*` for all); alerts from any other source are dropped.
2. **Scenes** of the channel: a sprite on certain days between two times, optionally with its own
   brightness or switched off.
3. **Ambient**: the channel's pictures from `/data/ambient/<channel>/` (or `/data/ambient/` when it
   has none) in turn, alternating with the display's own clock.

Brightness follows the sun at the configured place (day, civil twilight, night), unless a scene sets
its own. Every change through the API is saved and applies at once. Runs on the Raspberry from
[rpi-services](https://github.com/PlatanosVerdes/rpi-services), CalVer tag on every push to main.

## Web UI

`web/` holds the UI: plain files and [Lit](https://lit.dev) with no build step, embedded in the
binary and served at `/`. `/api` and `/healthz` keep priority and any other path answers with the
page. It shows the live preview, switches channels, edits channels, scenes, pictures, the schedule
and the settings, and sends test alerts. Spanish for browsers set to Spanish, English otherwise.
Browsers only give the device's location to HTTPS pages, so "use this device's location" needs one.

## The bridge

Flash an ESP32 with Bluetooth Classic (the original ESP32, not S3/C3/C6) with
[esp32-divoom](https://github.com/d03n3rfr1tz3/esp32-divoom). `firmware/scan` is an Arduino sketch
that lists nearby Bluetooth devices, to find the display's address. A display paired with a password
in the Divoom app needs the bridge's `bt_pin` setting, and a bridge on a weak WiFi signal may need a
higher `wifi_power`.

### A Linux machine as the bridge

`pixelbridge`, built into the same image, speaks the same TCP protocol over the machine's own
Bluetooth (an RFCOMM socket, no BlueZ D-Bus needed), so no ESP32 is needed while the display is in
reach. Pair the display once with `bluetoothctl` (`pair` with the display's PIN, then `trust`), then
run it on the host network and point pixelhub at it:

```sh
docker run --network host ghcr.io/platanosverdes/pixelhub pixelbridge -listen 172.17.0.1:7777
PIXELHUB_BRIDGE=host.docker.internal:7777   # in pixelhub, with extra_hosts host.docker.internal:host-gateway
```

Bluetooth sockets only work in the host's network namespace, which is why the bridge runs apart
from pixelhub. It needs no extra capabilities.

pixelbridge can also play a chime on the display's own speaker, through the host's PipeWire, when an
event starts. Set `PIXELHUB_CHIME=1` in pixelhub, and run the bridge as the user that runs PipeWire
with its runtime directory: `--user 1000:1000 -v /run/user:/run/user:ro -e XDG_RUNTIME_DIR=/run/user/1000
--tmpfs /tmp`. The display must be connected as an audio sink too, which it is once paired.

### A Mac as the bridge

`bridges/macos/pixelbridge.swift` speaks the same TCP protocol over the Mac's own Bluetooth, so no
ESP32 is needed while the display is near the Mac. Pair the display in System Settings first, then:

```sh
cd bridges/macos && swiftc -O pixelbridge.swift -o pixelbridge && ./pixelbridge   # 127.0.0.1:7777
PIXELHUB_BRIDGE=127.0.0.1:7777 PIXELHUB_DEVICE_MAC=01:12:23:45:56:67 go run ./cmd/pixelhub
```

A display that is also a headset shares its serial channel with Hands-Free, which macOS opens by
itself; the bridge drops that link before opening the serial one, so the display cannot play the
Mac's sound while the bridge holds it. It logs every frame it sends and receives.

## Running it

```sh
export PIXELHUB_BRIDGE=192.0.2.10:7777 PIXELHUB_DEVICE_MAC=01:12:23:45:56:67
PIXELHUB_PORT=8765 PIXELHUB_DATA_DIR=./data go run ./cmd/pixelhub
go run ./cmd/pixelhub demo     # every sprite for 2 s, then the clock; -bridge and -mac override the env
```

Only one client should talk to the bridge at a time: stop the service before running the demo.

## Configuration

The environment only says where the hardware is; everything else is state, edited through the API.

| Variable | Default | What it does |
| :--- | :--- | :--- |
| `PIXELHUB_BRIDGE` | required | Bridge `host:port`; use an IP, mDNS does not resolve inside containers |
| `PIXELHUB_DEVICE_MAC` | required | The display's Bluetooth address |
| `PIXELHUB_CHANNEL` | `1` | The display's RFCOMM channel |
| `PIXELHUB_PORT` | `8000` | HTTP port |
| `PIXELHUB_DATA_DIR` | `/data` | Holds `state.json`, `spotify.json`, `lists/`, `drawings/`, `sounds/`, `gallery-index.json`, `sprites/` and `ambient/` |
| `PIXELHUB_LOG_JSON` | | `1` logs JSON (set in the image) |
| `PIXELHUB_CHIME` | | `1` asks pixelbridge for a chime when a calendar event starts, except 23:00-08:00, and lets Mosko talk; the ESP32 bridge cannot play sound |
| `PIXELHUB_SPEECH` | | A [Wyoming](https://github.com/rhasspy/wyoming) text-to-speech server's `host:port`, such as `wyoming-piper`, for Mosko to read text |
| `PIXELHUB_VOICE` | | One of that server's voices, e.g. `es_ES-davefx-medium`; its default otherwise |
| `TZ` | | Time zone for scenes, the schedule and the sun, e.g. `Europe/London` |

`state.json` is written atomically. A fresh install has one channel, `clock`, that shows every alert
and the clock. A file that cannot be read is kept as `state.json.corrupt-<time>` and the service
starts from that fresh state.

## API

Plain HTTP and JSON, no auth: it is only reachable on the LAN and Tailscale. Errors answer
`{"error": "..."}` with 400 (invalid), 404, 409 (conflict, or a picture name already taken) or 413
(upload over 1 MB or 512x512).

| Endpoint | What it does |
| :--- | :--- |
| `GET /healthz` | 200 while the process runs, even with the display unreachable |
| `GET /api/status` | Channel and why (`override` or `schedule`), layer, what is showing, `preview_version`, connection, last ack, `released_until` |
| `POST /api/notify` | Show an alert: `sprite` or `color`, `priority` 0-10, `duration` up to `1h`, `source` |
| `POST /api/talk` | Mosko says `{"text": ...}` or a multipart `audio` recording, his mouth and eyes moving with it |
| `POST /api/channel` | Override the channel: `name`, optional `for`; without it, until the schedule changes |
| `DELETE /api/channel` | Back to the schedule |
| `POST /api/release`, `POST /api/claim` | Let the phone app have the display, and take it back (it comes back by itself after `release_for`) |
| `POST /api/reconnect` | Look for the display now: the wait between connect attempts grows to a minute while it is away |
| `GET /api/state` | Everything that is saved |
| `PUT /api/settings` | Any of `location`, `brightness`, `release_for`, `default_channel`, `music`, `clock`; fields left out, nested ones too, stay as they are (`music` and `clock` are replaced whole) |
| `GET /api/weather` | The weather the clock shows: `celsius`, `sky`, when it was read, and an `error` if the last read failed |
| `GET /api/channels`, `POST /api/channels` | List, create |
| `PUT /api/channels/{name}` | Replace; a different `name` renames it, its schedule slots, its pictures and an override on it. A missing `name` or `ambient` keeps the channel's own |
| `DELETE /api/channels/{name}` | Delete with its schedule slots and pictures; not the default or the last one |
| `PUT /api/schedule` | Replace the channel schedule |
| `GET /api/modes` | How many styles each display mode has |
| `PUT /api/mode` | Show one of the display's own modes until cleared: `{"mode": ...}` plus its options (below) |
| `DELETE /api/mode` | Back to alerts, scenes and ambient |
| `GET /api/gallery/categories` | Categories of the Divoom community gallery |
| `GET /api/gallery?category=&sort=likes\|new&page=` | One page of 16x16 pictures and animations: `file`, `name`, `author`, `likes` |
| `GET /api/gallery/search?q=&page=` | The gallery's own search (`SearchGalleryV2`, no account needed, `"source": "divoom"`); when it fails, `"source": "index"` and words looked for at the start of the names, authors, tags and descriptions of the 16 most liked and 8 newest pages of every category (about 11,000 pictures) and of every page browsed, read one page every 3 s from the first search on, again every 6 h, and kept across restarts (the gallery has no search without signing in and answers 429 to bursts); `indexed`/`of` say how far it got |
| `GET /api/gallery/gif?file=` | A gallery file as an animated GIF |
| `POST /api/gallery/show` | `{"file", "name", "for"}`: show it now, like an alert that every profile accepts (30 s by default) |
| `POST /api/gallery/save` | `{"file", "name", "channel"}`: keep it among a profile's ambient pictures |
| `POST /api/lists/{name}/upload` | A GIF or PNG of your own into a list (multipart field `file`) |
| `GET /api/lists`, `POST /api/lists` | Your lists of gallery pictures, and a new one (`{"name"}`); GIFs are kept in `/data/lists/`, so they stay if the gallery drops them |
| `PUT /api/lists/{name}`, `DELETE /api/lists/{name}` | Rename (`{"name"}`), delete |
| `POST /api/lists/{name}/items`, `DELETE /api/lists/{name}/items?file=` | Add a gallery picture (`{"file", "name", "author", "likes"}`), remove one |
| `GET /api/drawings`, `POST /api/drawings` | Animations drawn in the page (Draw tab): `name`, `delay_ms` 20-5000, `frames` (1-60, each 256 pixels as `rrggbb`, row by row); list, create |
| `PUT /api/drawings/{id}`, `DELETE /api/drawings/{id}` | Replace, delete |
| `POST /api/drawings/show` | A drawing, saved or not, plus `for` (30 s by default): show it now, like a gallery picture |
| `POST /api/drawings/{id}/list` | `{"list"}`: put a saved drawing in a list, so the automatic rotation shows it; again after an edit updates it |
| `DELETE /api/show` | Take off what was shown by hand (a gallery picture or a drawing) |
| `GET /api/sounds`, `POST /api/sounds` | Mosko's soundboard; a new one is `{"name", "text"}` or the multipart fields `name` and `audio` |
| `DELETE /api/sounds/{id}`, `POST /api/sounds/{id}/play` | Delete one, or have Mosko say it with the voice settings of now |
| `GET /api/spotify` | Whether a Spotify app and account are linked, and what plays now |
| `PUT /api/spotify` | `{"client_id": ...}` of your Spotify app; a new one forgets the signed-in account |
| `GET /api/spotify/login` | Sends the browser to Spotify to sign in; it comes back to `/api/spotify/callback` on the same host |
| `DELETE /api/spotify` | Forget the signed-in account |
| `GET /api/google` | Whether a Google client and account are linked, the account's calendars and which are followed, and the next events with their reminders (`alarms`, minutes before); never the secret or the tokens |
| `PUT /api/google` | `{"client_id", "client_secret"}` of your Google Cloud OAuth client (web application, Calendar API on, published so sign-ins do not expire after 7 days); a new one forgets the signed-in account |
| `GET /api/google/login` | Sends the browser to Google to sign in (read-only calendar access); it comes back to `/api/google/callback` on the same host, which must be https |
| `PUT /api/google/calendars` | `{"ids": [...]}`: the calendars to follow; the primary one is followed after signing in |
| `DELETE /api/google` | Forget the signed-in account |
| `POST /api/google/show` | Show the warning the next event will get, for 15 s |
| `GET /api/channels/{name}/pictures/{file}` | A picture's first frame, drawn 10x like a sprite |
| `GET/POST /api/channels/{name}/pictures` | List, upload (multipart field `file`, GIF or PNG, up to 1 MB and 512x512; the name is cleaned up and must be new) |
| `DELETE /api/channels/{name}/pictures/{file}` | Delete a picture |
| `GET /api/sprites`, `GET /api/sprites/{name}.png` | Sprite names, and each one drawn 10x |
| `GET /api/preview.png` | What the display should show now, drawn 10x; refetch when `preview_version` changes, or send `If-None-Match` with its `ETag` |

```sh
curl -X POST localhost:8000/api/notify -d '{"sprite":"heart","source":"doorbell"}'
curl -X POST localhost:8000/api/notify -d '{"color":[255,0,0],"priority":9,"duration":"30s"}'
curl -X POST localhost:8000/api/channel -d '{"name":"evening","for":"4h"}'
curl -X POST localhost:8000/api/channels -d '{"name":"office","sources":["office","system"],
  "ambient":{"pictures":"2m","clock":"3m"},
  "scenes":[{"name":"lunch","days":"mon-fri","from":"14:00","to":"15:00","sprite":"lunch"}]}'
curl -X PUT localhost:8000/api/schedule -d '[{"channel":"office","days":"mon-fri","from":"09:00","to":"18:00"}]'
curl -X PUT localhost:8000/api/settings -d '{"location":{"latitude":40.4,"longitude":-3.7},"default_channel":"clock"}'
curl -F file=@cat.gif localhost:8000/api/channels/office/pictures
curl -o now.png localhost:8000/api/preview.png
```

Times are `HH:MM` (`24:00` ends a range at midnight); a range past midnight belongs to the day it
starts on. Days are `mon-fri`, `sat,sun`, `daily`. Durations are Go durations like `90s` or `2m`. A
scene's `brightness` is 0-100 or `"off"`; without it the sun decides, and a scene without a sprite
shows the clock. An ambient `pictures` or `clock` of `0s` skips that part. An ambient or a scene
with `"list"` shows the pictures of that list in turn instead (the web uses the default channel
this way: its ambient list and its scenes are "In automatic" and "Schedule" on the Display tab). An alert without a source
only shows on channels that accept `*`, and a channel with no sources is a quiet one; a dropped
alert answers 200 with `"dropped": true`. A channel created without `ambient` alternates 2 minutes of
pictures with 3 of the clock.

### Battery

A bridge that can read the display's battery sends `0x42` and the level, 0 (empty) to 9 (full).
The display only reports it over hands-free, to a gateway that answers `AT+XAPL` as an iPhone; the
macOS bridge cannot, an ESP32 bridge with hands-free can. pixelhub shows it in the status
(`battery`, `battery_at`) and on the display: a full battery once when it gets there, a low one
(1 or 0) at once and every hour while it stays low.

### Room sensors

A light sensor and a sound sensor, on the bridge (`0x4c` and the light 0-255, `0x53` and any byte
for a sound) or anywhere else (`POST /api/sensors` with `{"light": 0-255, "sound": true}`), make
the room count. While a light reading is under two minutes old, brightness goes from the night
level in the dark to the day level in full light, instead of following the sun. A sound while a
schedule exception keeps the display off shows the time with a "zz", at brightness 5 for 10 s, at
most every 30 s.

### Display modes

The display's own modes, the ones its side button cycles through. A mode set with `PUT /api/mode`
shows over scenes and ambient and is saved; alerts still show on top. A scene can show one too with
`"view"` instead of `"sprite"`.

| `mode` | Options |
| :--- | :--- |
| `clock` | `style` 0-5 (full screen, rainbow, boxed, analog round, negative, analog square), `weather`, `temperature`, `calendar`, `color` `#rrggbb` (replaces the red parts) |
| `light` | `color`, `style` 0-4 (plain, love, plants, no mosquitoes, sleep) |
| `visualizer` | `style` 0-11; it follows what the display plays as a speaker |
| `animations` | none: plays the list kept on the display from the Divoom app, which pixelhub never writes |
| `scoreboard` | `red`, `blue`, 0-999 |
| `stopwatch` | `running` |
| `countdown` | `seconds` 1-5999, `running` |
| `noise` | `running`: the noise meter, with the display's microphone |

A running stopwatch, countdown or noise meter is stopped when something else takes the screen.

The clock pixelhub shows between pictures, and any clock without a `color`, takes its look from
`"clock"` in `PUT /api/settings` (a `clock` view; red until set), so the look kept on the display
from the Divoom app never shows through. With `weather` or `temperature` on, the clock gets the
current weather for `location` from [Open-Meteo](https://open-meteo.com) every 15 minutes.

```sh
curl -X PUT localhost:8000/api/mode -d '{"mode":"clock","style":1,"calendar":true}'
curl -X DELETE localhost:8000/api/mode
```

### Spotify

pixelhub can follow what a Spotify account plays on any of its devices and show the album cover, or
a display mode, while it plays (`"music": {"enabled": true, "view": {...}, "devices": [...]}` in `PUT /api/settings`;
with `devices`, only what plays on those Spotify devices shows, so music played away from home does
not reach the display at home; `GET /api/spotify` lists the devices seen).
It sits under alerts and a picked mode, over scenes and ambient. Create an app at
developer.spotify.com/dashboard with the Web API, add `<pixelhub URL>/api/spotify/callback` as a
redirect URI (Spotify takes `http://127.0.0.1:<port>` but not `localhost`, and otherwise wants
HTTPS), and paste its client ID in Settings. Sign-in uses PKCE, so no client secret is stored; the
refresh token is kept in `/data/spotify.json` (mode 0600). Development-mode apps need a Premium
account. Spotify no longer gives beat or tempo data to new apps.

### Mosko

Mosko is a face that talks on the display: `POST /api/talk` with `{"text": "..."}` (needs
`PIXELHUB_SPEECH`) or a recording in the multipart field `audio` (anything ffmpeg reads, up to 12 MB),
at most 30 s. It needs pixelbridge, which plays the sound.

- **Mouth**: follows the voice's loudness in 10 ms windows, shut under a gate set from the clip's own
  quiet and loud parts, then three openings; long silences get a blink.
- **Gestures**: text is read a sentence at a time, so each ending sets the eyes where it is said:
  `!` raises the eyebrows, `?` one of them, `...` a long pause looking aside, and `jaja` laughs.
- **Timing**: pixelhub sends the sound and every face with its time in one message; pixelbridge starts
  the sound and paints each face as a still on its own clock, so there is no 60-frame limit and no
  drift. Anything else painted meanwhile stops both. The sound starts with 0.8 s of silence, which the
  speaker's Bluetooth stream eats while it wakes up.
- **Settings** (`"talk"` in `PUT /api/settings`): `lag_ms`, how much later the speaker sounds than the
  display paints (250 by default: PipeWire alone reports 140 ms); `pitch` in percent at the same
  speed; `effect`, one of `cartoon`, `squirrel`, `monster`, `robot`, `nervous`, `echo`.

The container needs a writable `/tmp` for ffmpeg.

## Sprites

Built in: `heart`, `heartbeat`, `moon`, `sun`, `sunrise`, `lunch`, `coffee`, `zzz`, `alert`, `check`.
Any `/data/sprites/<name>.gif` or `.png` adds a sprite or replaces a built-in one of the same name;
they are read at start. Pictures that are not 16x16 are resized (nearest neighbour). Animations keep
their first 60 frames, fewer if they would not fit in 256 chunks.

## Bridge protocol

Learnt on the real hardware; the code lives in `internal/bridge` (session) and `internal/divoom`
(encoding).

- **Connect**: `69` + 6 MAC bytes + channel. The link takes about 6 s and is up only when the
  display's greeting arrives (`01 09 00 04 bd 55 ...`); anything sent before it is dropped.
- **Never connect while connected**: the bridge drops and redials and the display often refuses.
  pixelhub always probes first.
- **Probe and keepalive**: brightness `74 <0-100>`. Connected: a response `01 .. .. 04 <cmd> 55 ...`
  plus a state frame. Disconnected: a single `96` byte, which the bridge also sends on its own when
  the link drops.
- **Release**: `96` + 6 MAC bytes.
- **Frames**: `01`, length (uint16 LE, payload + 3), command, payload, checksum (uint16 LE, sum of
  length, command and payload bytes), `02`. Several can share a TCP read and one can span reads.
- **Commands**: light `45 01 R G B <brightness> 00 01 00 00 00`, clock `45 00 01 00 01 00 00 00 00 00 00 00`,
  still image `44 00 0A 0A 04 <frame>`, animation `49 <total uint16 LE> <chunk index> <200 bytes>` per
  chunk, about 50 ms apart, at most 60 frames.
- **Sound** (pixelbridge only): `7c` plays the chime. `7d` and the length of the rest (uint32 LE) is a
  talk: the waits before the sound and before the faces in ms (uint16 LE each), the WAV's length
  (uint32 LE) and the WAV, then per face its time on screen in ms (uint16 LE), its length (uint16 LE)
  and the message that paints it. The display greets each face; the bridge keeps those greetings
  from pixelhub while a talk runs.
- **Picture frame**: `AA`, length (uint16 LE, body + 3), then the delay in ms (uint16 LE, 0 for a still),
  `00`, colour count (256 is written as 0), RGB palette in first-seen order, and the pixel indices packed
  LSB first at `max(1, ceil(log2(colours)))` bits each. Pixels with alpha up to 32 are black.

## Development

```sh
go test ./...
```
