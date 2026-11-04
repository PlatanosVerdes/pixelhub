package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()
	valid := map[string]string{"PIXELHUB_BRIDGE": "192.0.2.10:7777", "PIXELHUB_DEVICE_MAC": "01:12:23:45:56:67"}
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"minimal", valid, ""},
		{"no bridge", map[string]string{"PIXELHUB_DEVICE_MAC": "01:12:23:45:56:67"}, "PIXELHUB_BRIDGE"},
		{"bridge without port", map[string]string{"PIXELHUB_BRIDGE": "192.0.2.10", "PIXELHUB_DEVICE_MAC": "01:12:23:45:56:67"}, "PIXELHUB_BRIDGE"},
		{"no mac", map[string]string{"PIXELHUB_BRIDGE": "192.0.2.10:7777"}, "PIXELHUB_DEVICE_MAC"},
		{"long mac", map[string]string{"PIXELHUB_BRIDGE": "192.0.2.10:7777", "PIXELHUB_DEVICE_MAC": "01:12:23:45:56:67:78:89"}, "PIXELHUB_DEVICE_MAC"},
		{"nothing set names both", map[string]string{}, "PIXELHUB_DEVICE_MAC"},
		{"bad channel", with(valid, "PIXELHUB_CHANNEL", "0"), "PIXELHUB_CHANNEL"},
		{"bad port", with(valid, "PIXELHUB_PORT", "http"), "PIXELHUB_PORT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := Load(func(k string) string { return tt.env[k] })
			if tt.want == "" {
				if err != nil || c.Port != 8000 || c.DataDir != "/data" || c.Channel != 1 || c.DeviceMAC.String() != "01:12:23:45:56:67" {
					t.Errorf("got %+v, %v", c, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want one mentioning %s", err, tt.want)
			}
		})
	}
}

func with(env map[string]string, key, value string) map[string]string {
	out := map[string]string{key: value}
	for k, v := range env {
		if k != key {
			out[k] = v
		}
	}
	return out
}
