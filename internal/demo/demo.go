package demo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/bridge"
	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
)

type Options struct {
	Bridge     bridge.Options
	Sprites    *sprite.Library
	Brightness int
	Hold       time.Duration
}

// Run paints every built-in sprite, the beating heart and the clock, then disconnects from the bridge.
func Run(ctx context.Context, opt Options, log *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	session := bridge.New(opt.Bridge, log)
	clock := display.ClockContent().WithBrightness(opt.Brightness)
	session.Show(clock)
	done := make(chan struct{})
	go func() {
		session.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	wait, stop := context.WithTimeout(ctx, time.Minute)
	defer stop()
	if err := session.WaitConnected(wait); err != nil {
		return fmt.Errorf("connect to the Evo through %s: %w", opt.Bridge.Address, err)
	}
	var names []string
	for _, name := range opt.Sprites.Names() {
		if name != "heartbeat" {
			names = append(names, name)
		}
	}
	for _, name := range append(names, "heartbeat") {
		content, _ := opt.Sprites.Get(name)
		if err := show(ctx, session, content.WithBrightness(opt.Brightness), opt.Hold, log); err != nil {
			return err
		}
	}
	return show(ctx, session, clock, opt.Hold, log)
}

func show(ctx context.Context, s *bridge.Session, c display.Content, hold time.Duration, log *slog.Logger) error {
	log.Info("demo", "showing", c.Name)
	s.Show(c)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(hold):
	}
	if !s.Status().Connected {
		return fmt.Errorf("lost the Evo while showing %s", c.Name)
	}
	return nil
}
