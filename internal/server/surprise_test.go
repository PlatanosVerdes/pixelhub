package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

func TestMomentThemes(t *testing.T) {
	t.Parallel()
	at := func(s string) time.Time {
		d, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	has := func(themes []theme, label string) bool {
		for _, th := range themes {
			if th.Label == label {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		when    string
		weather display.Weather
		want    []string
		not     []string
	}{
		{"2026-10-31 21:00", display.Weather{}, []string{"Halloween", "Noche", "Otoño"}, []string{"Navidad"}},
		{"2026-12-24 08:00", display.Weather{}, []string{"Navidad", "Buenos días", "Invierno"}, []string{"Halloween"}},
		{"2026-11-08 14:00", display.Weather{Known: true, Sky: display.SkyRain}, []string{"Hora de comer", "Fin de semana", "Llueve"}, []string{"Halloween", "Navidad"}},
		{"2026-04-04 12:00", display.Weather{}, []string{"Pascua", "Primavera"}, nil},
		{"2026-07-10 23:30", display.Weather{}, []string{"A dormir", "Verano"}, nil},
	} {
		got := momentThemes(at(c.when), c.weather)
		for _, w := range c.want {
			if !has(got, w) {
				t.Errorf("%s: no %q in %v", c.when, w, got)
			}
		}
		for _, w := range c.not {
			if has(got, w) {
				t.Errorf("%s: %q in %v", c.when, w, got)
			}
		}
	}
}

func TestSurprise(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	srv := &Server{Hub: f.hub, Gallery: fakeGallery{}, GalleryPace: 10 * time.Millisecond, Size: 16}
	h := srv.Handler()
	for _, kind := range []string{"random", "moment"} {
		rec := serve(t, h, "POST", "/api/gallery/surprise", `{"kind":"`+kind+`","for":"10s"}`)
		if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"theme"`) {
			t.Errorf("%s: %d %s", kind, rec.Code, rec.Body)
		}
	}
	if rec := serve(t, h, "POST", "/api/gallery/surprise", `{"kind":"nope"}`); rec.Code != 400 {
		t.Errorf("an unknown kind: %d %s", rec.Code, rec.Body)
	}

	if _, ok, _ := srv.Surprise(Random); ok {
		t.Fatal("a random picture before any was fetched")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.RunSurprises(ctx)
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, ok, _ := srv.Surprise(Random); ok {
			if c.Key == "" || len(c.Frames) == 0 {
				t.Errorf("picked %+v", c)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RunSurprises fetched nothing")
		}
	}
}
