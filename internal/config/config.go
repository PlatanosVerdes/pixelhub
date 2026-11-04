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
	return c, errors.Join(errs...)
}
