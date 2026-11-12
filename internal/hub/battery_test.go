package hub

import (
	"testing"
	"time"
)

func TestBatteryWatch(t *testing.T) {
	t.Parallel()
	type reading struct {
		at    time.Duration
		level int
		want  string
	}
	m := time.Minute
	tests := []struct {
		name     string
		readings []reading
	}{
		{"charge me on reaching 1, every 5 min while there", []reading{
			{0, 3, ""},
			{m, 2, ""},
			{2 * m, 1, "battery-charge"},
			{3 * m, 1, ""},
			{6 * m, 1, ""},
			{7 * m, 1, "battery-charge"},
			{8 * m, 1, ""},
			{12 * m, 1, "battery-charge"},
		}},
		{"dying at 0, every minute", []reading{
			{0, 1, "battery-charge"},
			{m, 0, "battery-dying"},
			{m + 30*time.Second, 0, ""},
			{2 * m, 0, "battery-dying"},
			{3 * m, 0, "battery-dying"},
		}},
		{"stops when the level leaves the band", []reading{
			{0, 0, "battery-dying"},
			{m, 2, ""},
			{6 * m, 2, ""},
			{60 * m, 3, ""},
		}},
		{"back into a band shows at once", []reading{
			{0, 1, "battery-charge"},
			{m, 2, ""},
			{2 * m, 1, "battery-charge"},
		}},
		{"unplug me on climbing to 8, every 15 min while at 8 or more", []reading{
			{0, 7, ""},
			{m, 8, "battery-unplug"},
			{2 * m, 9, ""},
			{16 * m, 9, "battery-unplug"},
			{20 * m, 7, ""},
			{21 * m, 8, "battery-unplug"},
		}},
		{"full when first seen is not news until it leaves and comes back", []reading{
			{0, 9, ""},
			{20 * m, 9, ""},
			{60 * m, 8, ""},
			{90 * m, 7, ""},
			{120 * m, 8, "battery-unplug"},
		}},
		{"low when first seen is news", []reading{
			{0, 1, "battery-charge"},
		}},
	}
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var w batteryWatch
			for _, r := range tt.readings {
				if got := w.see(r.level, t0.Add(r.at)); got != r.want {
					t.Errorf("level %d at %s shows %q, want %q", r.level, r.at, got, r.want)
				}
			}
		})
	}
}
