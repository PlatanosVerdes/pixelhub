package sun

import (
	"testing"
	"time"
)

const greenwichLat, greenwichLon = 51.4769, -0.0005

func TestTimesInGreenwich(t *testing.T) {
	t.Parallel()
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		day       time.Time
		rise, set string
	}{
		{"summer solstice", time.Date(2026, 6, 21, 12, 0, 0, 0, london), "04:43", "21:21"},
		{"winter solstice", time.Date(2026, 12, 21, 12, 0, 0, 0, london), "08:04", "15:53"},
		{"equinox", time.Date(2026, 3, 20, 12, 0, 0, 0, london), "06:03", "18:13"},
		{"late night still counts for its own day", time.Date(2026, 11, 4, 23, 59, 0, 0, london), "07:00", "16:27"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rise, set, ok := Times(tt.day, greenwichLat, greenwichLon)
			if !ok {
				t.Fatal("no sunrise")
			}
			near(t, "sunrise", rise, tt.day, tt.rise)
			near(t, "sunset", set, tt.day, tt.set)
		})
	}
}

func near(t *testing.T, what string, got, day time.Time, want string) {
	t.Helper()
	clock, err := time.Parse("15:04", want)
	if err != nil {
		t.Fatal(err)
	}
	expected := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, day.Location())
	if diff := got.Sub(expected).Abs(); diff > 3*time.Minute {
		t.Errorf("%s %s, want %s (off by %s)", what, got.Format("15:04"), want, diff)
	}
}

func TestPhaseAt(t *testing.T) {
	t.Parallel()
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	at := func(hour, minute int) time.Time { return time.Date(2026, 11, 4, hour, minute, 0, 0, london) }
	tests := []struct {
		at   time.Time
		want Phase
	}{
		{at(3, 0), Night},
		{at(6, 40), Dusk},
		{at(12, 0), Day},
		{at(16, 40), Dusk},
		{at(18, 0), Night},
	}
	for _, tt := range tests {
		t.Run(tt.at.Format("15:04"), func(t *testing.T) {
			t.Parallel()
			if got := PhaseAt(tt.at, greenwichLat, greenwichLon); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPolarPhases(t *testing.T) {
	t.Parallel()
	june := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	december := time.Date(2026, 12, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		at   time.Time
		lat  float64
		want Phase
	}{
		{"arctic summer", june, 78, Day},
		{"arctic winter", december, 78, Night},
		{"antarctic summer", december, -78, Day},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PhaseAt(tt.at, tt.lat, 15); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}
