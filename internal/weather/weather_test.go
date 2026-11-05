package weather

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

func TestRefresh(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("latitude") != "39.5700" {
			http.Error(w, "wrong place", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"current":{"temperature_2m":-2.6,"weather_code":73}}`))
	}))
	defer srv.Close()
	place := Place{39.57, 2.65}
	s := New(func() Place { return place }, slog.New(slog.DiscardHandler))
	s.API = srv.URL
	s.refresh(context.Background())
	if got := s.Current(); got != (display.Weather{Known: true, Celsius: -3, Sky: display.SkySnow}) {
		t.Errorf("got %+v", got)
	}
	place = Place{40, 3}
	if s.Current().Known {
		t.Error("a reading for another place is not current")
	}
	s.refresh(context.Background())
	if r := s.Reading(); r.Error == "" || r.Known {
		t.Errorf("a failed read for the new place: %+v", r)
	}
}

func TestSky(t *testing.T) {
	t.Parallel()
	for code, want := range map[int]display.Sky{0: display.SkyClear, 2: display.SkyCloudy, 45: display.SkyFog, 61: display.SkyRain, 81: display.SkyRain, 75: display.SkySnow, 95: display.SkyStorm} {
		if got := Sky(code); got != want {
			t.Errorf("code %d: %d, want %d", code, got, want)
		}
	}
}
