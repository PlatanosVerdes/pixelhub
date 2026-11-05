// Package weather reads the current temperature and sky for a place from Open-Meteo, which needs no key.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	DefaultAPI = "https://api.open-meteo.com/v1/forecast"
	every      = 15 * time.Minute
	// A reading older than this is not shown, rather than show stale weather as current.
	maxAge = 2 * time.Hour
)

type Place struct{ Latitude, Longitude float64 }

type Reading struct {
	display.Weather
	At    time.Time `json:"at,omitzero"`
	Place Place     `json:"place"`
	Error string    `json:"error,omitempty"`
}

type Station struct {
	API   string
	HTTP  *http.Client
	Where func() Place
	Log   *slog.Logger

	mu    sync.Mutex
	last  Reading
	tried Place
	next  time.Time
}

func New(where func() Place, log *slog.Logger) *Station {
	return &Station{API: DefaultAPI, HTTP: &http.Client{Timeout: 15 * time.Second}, Where: where, Log: log}
}

// Current is the latest reading for the display, unknown when there is none or it is too old.
func (s *Station) Current() display.Weather {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last.At.IsZero() || time.Since(s.last.At) > maxAge || s.last.Place != s.Where() {
		return display.Weather{}
	}
	return s.last.Weather
}

func (s *Station) Reading() Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Run reads the weather every 15 minutes, and at once when the place changes.
func (s *Station) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		s.mu.Lock()
		due := s.tried != s.Where() || !time.Now().Before(s.next)
		s.mu.Unlock()
		if due {
			s.refresh(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Station) refresh(ctx context.Context) {
	place := s.Where()
	w, err := s.fetch(ctx, place)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tried = place
	if err != nil {
		s.Log.Warn("weather not read", "err", err)
		if s.last.Place != place {
			s.last = Reading{Place: place}
		}
		s.last.Error = err.Error()
		s.next = time.Now().Add(3 * time.Minute)
		return
	}
	s.last = Reading{Weather: w, At: time.Now(), Place: place}
	s.next = time.Now().Add(every)
}

func (s *Station) fetch(ctx context.Context, p Place) (display.Weather, error) {
	q := url.Values{
		"latitude":  {fmt.Sprintf("%.4f", p.Latitude)},
		"longitude": {fmt.Sprintf("%.4f", p.Longitude)},
		"current":   {"temperature_2m,weather_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.API+"?"+q.Encode(), nil)
	if err != nil {
		return display.Weather{}, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return display.Weather{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return display.Weather{}, fmt.Errorf("weather: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Code        int     `json:"weather_code"`
		} `json:"current"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
		return display.Weather{}, fmt.Errorf("weather: %w", err)
	}
	return display.Weather{Known: true, Celsius: int(math.Round(out.Current.Temperature)), Sky: Sky(out.Current.Code)}, nil
}

// Sky turns a WMO weather code into the display's own.
func Sky(code int) display.Sky {
	switch {
	case code <= 1:
		return display.SkyClear
	case code <= 3:
		return display.SkyCloudy
	case code == 45 || code == 48:
		return display.SkyFog
	case code >= 95:
		return display.SkyStorm
	case code >= 71 && code <= 77, code == 85, code == 86:
		return display.SkySnow
	}
	return display.SkyRain
}
