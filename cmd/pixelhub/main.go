package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/bridge"
	"github.com/PlatanosVerdes/pixelhub/internal/calendar"
	"github.com/PlatanosVerdes/pixelhub/internal/config"
	"github.com/PlatanosVerdes/pixelhub/internal/demo"
	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/divoom"
	"github.com/PlatanosVerdes/pixelhub/internal/drawings"
	"github.com/PlatanosVerdes/pixelhub/internal/gallery"
	"github.com/PlatanosVerdes/pixelhub/internal/hub"
	"github.com/PlatanosVerdes/pixelhub/internal/lists"
	"github.com/PlatanosVerdes/pixelhub/internal/server"
	"github.com/PlatanosVerdes/pixelhub/internal/sounds"
	"github.com/PlatanosVerdes/pixelhub/internal/spotify"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
	"github.com/PlatanosVerdes/pixelhub/internal/talk"
	"github.com/PlatanosVerdes/pixelhub/internal/weather"
)

var buildVersion = "local"

const (
	calendarPriority = 5
	calendarAlert    = 30 * time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	getenv := os.Getenv
	var demoOpt *demoFlags
	if len(args) > 0 {
		if args[0] != "demo" {
			return fmt.Errorf("unknown command %q, want demo or nothing", args[0])
		}
		var err error
		if getenv, demoOpt, err = parseDemo(args[1:]); err != nil {
			return err
		}
	}
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if cfg.LogJSON {
		log = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sprites, err := sprite.Load(filepath.Join(cfg.DataDir, "sprites"), divoom.Size)
	if err != nil {
		return err
	}
	if demoOpt != nil {
		return demo.Run(ctx, demo.Options{
			Bridge:     bridge.DefaultOptions(cfg.Bridge, cfg.DeviceMAC, cfg.Channel),
			Sprites:    sprites,
			Brightness: demoOpt.brightness,
			Hold:       demoOpt.hold,
		}, log)
	}
	return serve(ctx, cfg, sprites, log)
}

func serve(ctx context.Context, cfg config.Config, sprites *sprite.Library, log *slog.Logger) error {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	store, st, err := state.Open(filepath.Join(cfg.DataDir, "state.json"), log)
	if err != nil {
		return err
	}
	opt := bridge.DefaultOptions(cfg.Bridge, cfg.DeviceMAC, cfg.Channel)
	opt.Chime = cfg.Chime
	session := bridge.New(opt, log)
	pictures := sprite.Ambient{Dir: filepath.Join(cfg.DataDir, "ambient"), Size: divoom.Size}
	h := hub.New(session, st, store, sprites, pictures, log)
	music, err := spotify.Open(filepath.Join(cfg.DataDir, "spotify.json"), log)
	if err != nil {
		return err
	}
	h.Music = music
	station := weather.New(func() weather.Place {
		l := h.State().Location
		return weather.Place{Latitude: l.Latitude, Longitude: l.Longitude}
	}, log)
	h.Weather = station
	kept, err := lists.Open(filepath.Join(cfg.DataDir, "lists"))
	if err != nil {
		return err
	}
	h.Lists = hub.ListFunc(func(list string, turn int) (display.Content, bool, error) { return kept.Pick(list, turn, divoom.Size) })
	cal, err := calendar.Open(filepath.Join(cfg.DataDir, "google.json"), log)
	if err != nil {
		return err
	}
	cal.Warn = func(e calendar.Event, left time.Duration) {
		_, shown := h.Notify(hub.Alert{Content: calendar.Picture(left, e.Title, divoom.Size), Priority: calendarPriority, Duration: calendarAlert, Source: "calendar"})
		if shown && left == 0 && !calendar.Quiet(time.Now()) {
			h.Chime()
		}
	}
	go session.Run(ctx)
	go h.Run(ctx)
	go music.Run(ctx)
	go station.Run(ctx)
	go cal.Run(ctx)

	api := &server.Server{Hub: h, Sprites: sprites, Spotify: music, Calendar: cal, Weather: station, Gallery: gallery.NewClient(), Lists: kept, IndexPath: filepath.Join(cfg.DataDir, "gallery-index.json"), Size: divoom.Size, Version: buildVersion}
	if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
		api.Decoder = talk.Decoder{FFmpeg: ffmpeg}
	} else {
		log.Info("no ffmpeg, Mosko stays quiet")
	}
	if cfg.Speech != "" {
		api.Voice = talk.Speaker{Addr: cfg.Speech, Voice: cfg.Voice}
	}
	if api.Drawings, err = drawings.Open(filepath.Join(cfg.DataDir, "drawings"), divoom.Size); err != nil {
		return err
	}
	if api.Sounds, err = sounds.Open(filepath.Join(cfg.DataDir, "sounds")); err != nil {
		return err
	}
	go api.WarmGallery(ctx)
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Info("pixelhub listening", "version", buildVersion, "port", cfg.Port, "bridge", cfg.Bridge,
		"channels", len(st.Channels), "sprites", len(sprites.Names()))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-stopped
	return nil
}

type demoFlags struct {
	brightness int
	hold       time.Duration
}

// parseDemo lets -bridge and -mac stand in for their environment variables.
func parseDemo(args []string) (func(string) string, *demoFlags, error) {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	address := fs.String("bridge", "", "bridge address host:port (default $PIXELHUB_BRIDGE)")
	mac := fs.String("mac", "", "display Bluetooth address (default $PIXELHUB_DEVICE_MAC)")
	opt := &demoFlags{}
	fs.IntVar(&opt.brightness, "brightness", 60, "brightness 0-100")
	fs.DurationVar(&opt.hold, "hold", 2*time.Second, "time each sprite stays on screen")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	getenv := func(key string) string {
		switch {
		case key == "PIXELHUB_BRIDGE" && *address != "":
			return *address
		case key == "PIXELHUB_DEVICE_MAC" && *mac != "":
			return *mac
		}
		return os.Getenv(key)
	}
	return getenv, opt, nil
}
