package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Config is the process setup from the environment; everything the owner edits lives in state.json.
type Config struct {
	Bridge    string
	DeviceMAC net.HardwareAddr
	Channel   byte
	Port      int
	DataDir   string
	LogJSON   bool
	// Chime lets pixelhub ask the bridge for a sound; only pixelbridge plays one.
	Chime bool
	// Speech is a Wyoming text-to-speech server's host:port, Voice one of its voices; both optional.
	Speech string
	Voice  string
	// Plug is the Tuya plug the display charges from, nil when charging is not managed.
	Plug *Plug
}

type Plug struct {
	ID, IP, Key string
}

func Load(getenv func(string) string) (Config, error) {
	env := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	c := Config{
		Bridge:  getenv("PIXELHUB_BRIDGE"),
		DataDir: env("PIXELHUB_DATA_DIR", "/data"),
		LogJSON: getenv("PIXELHUB_LOG_JSON") == "1",
		Chime:   getenv("PIXELHUB_CHIME") == "1",
		Speech:  getenv("PIXELHUB_SPEECH"),
		Voice:   getenv("PIXELHUB_VOICE"),
	}
	if c.Speech != "" {
		if _, _, err := net.SplitHostPort(c.Speech); err != nil {
			return c, errors.New("PIXELHUB_SPEECH: want the speech server's host:port like piper:10200")
		}
	}
	var errs []error
	if _, _, err := net.SplitHostPort(c.Bridge); err != nil {
		errs = append(errs, errors.New("PIXELHUB_BRIDGE: required, the bridge's host:port like 192.0.2.10:7777"))
	}
	mac, err := net.ParseMAC(getenv("PIXELHUB_DEVICE_MAC"))
	if err != nil || len(mac) != 6 {
		errs = append(errs, errors.New("PIXELHUB_DEVICE_MAC: required, the display's Bluetooth address like 01:12:23:45:56:67"))
	}
	c.DeviceMAC = mac
	channel, err := strconv.Atoi(env("PIXELHUB_CHANNEL", "1"))
	if err != nil || channel < 1 || channel > 30 {
		errs = append(errs, errors.New("PIXELHUB_CHANNEL: want a Bluetooth RFCOMM channel 1-30"))
	}
	c.Channel = byte(channel)
	if c.Port, err = strconv.Atoi(env("PIXELHUB_PORT", "8000")); err != nil || c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PIXELHUB_PORT: want a port number"))
	}
	c.Plug, err = loadPlug(getenv)
	return c, errors.Join(append(errs, err)...)
}

func loadPlug(getenv func(string) string) (*Plug, error) {
	p := Plug{ID: getenv("PIXELHUB_PLUG_ID"), IP: getenv("PIXELHUB_PLUG_IP"), Key: getenv("PIXELHUB_PLUG_KEY")}
	switch {
	case p == Plug{}:
		return nil, nil
	case p.ID == "" || p.IP == "" || p.Key == "":
		return nil, errors.New("PIXELHUB_PLUG_ID, PIXELHUB_PLUG_IP, PIXELHUB_PLUG_KEY: set all three to manage charging, or none")
	case net.ParseIP(p.IP) == nil:
		return nil, errors.New("PIXELHUB_PLUG_IP: want the plug's IP address like 192.0.2.20")
	case len(p.Key) != 16:
		return nil, errors.New("PIXELHUB_PLUG_KEY: want the plug's 16-character local key")
	}
	return &p, nil
}
