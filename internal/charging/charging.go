// Package charging keeps the display's battery between about 30 and 80 % by switching the plug its
// charger hangs from.
package charging

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	// The battery reads 0 (empty) to 9 (full); charging starts at or under onAt and stops at or over offAt.
	onAt, offAt = 2, 7
	// stale is how long without a reading before the plug goes on so the display never dies.
	stale = 2 * time.Hour
	every = time.Minute
	// reassert is how often, at most, the wanted state is sent again to a plug switched by hand.
	reassert = 10 * time.Minute
	// stuck is how long the plug can be on with the battery not climbing before it is not charging:
	// a 10 % step takes well under it, so the display is most likely not on the plug.
	stuck = time.Hour
	// fresh is how old a reading can be to say the battery is still not climbing.
	fresh = 30 * time.Minute
)

type Power int

const (
	Unknown Power = iota
	Off
	On
)

func powerOf(on bool) Power {
	if on {
		return On
	}
	return Off
}

// Want is the plug state the battery calls for. Between the thresholds it keeps last, Unknown for
// no opinion. since is when the reading came, or when watching started if none has.
func Want(level *int, since, now time.Time, last Power) Power {
	switch {
	case now.Sub(since) >= stale:
		return On
	case level == nil:
		return last
	case *level <= onAt:
		return On
	case *level >= offAt:
		return Off
	}
	return last
}

type Plug interface {
	Power(ctx context.Context) (bool, error)
	SetPower(ctx context.Context, on bool) error
}

type Battery interface {
	Status() display.Status
}

// Status is the plug as last read or switched, or why it could not be.
type Status struct {
	On    bool      `json:"on"`
	At    time.Time `json:"at,omitzero"`
	Error string    `json:"error,omitempty"`
	// NotCharging is the plug on for a while with the battery not climbing.
	NotCharging bool `json:"not_charging,omitempty"`
}

type Charger struct {
	plug    Plug
	battery Battery
	log     *slog.Logger
	now     func() time.Time
	started time.Time

	// want is the state the battery last called for, Unknown until it has; sentAt when it was last sent.
	want   Power
	sentAt time.Time
	// top is the highest level seen since the plug went on, and topAt when it was reached or the plug
	// went on; the zero topAt is the plug not on.
	top   int
	topAt time.Time

	mu     sync.Mutex
	status *Status
}

func New(plug Plug, battery Battery, log *slog.Logger) *Charger {
	return &Charger{plug: plug, battery: battery, log: log, now: time.Now, started: time.Now()}
}

// Status is nil until the plug has been tried once.
func (c *Charger) Status() *Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == nil {
		return nil
	}
	s := *c.status
	return &s
}

func (c *Charger) Run(ctx context.Context) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		c.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Charger) step(ctx context.Context) {
	now := c.now()
	on, err := c.plug.Power(ctx)
	if err != nil {
		c.failed(err)
		return
	}
	c.seen(now, on)
	st := c.battery.Status()
	since := st.BatteryAt
	if st.Battery == nil {
		since = c.started
	}
	c.watchCharge(now, on, st)
	want := Want(st.Battery, since, now, c.want)
	if want == Unknown || want == powerOf(on) {
		c.want = want
		return
	}
	// The plug was switched by hand since it was last told; it is told again, but not every minute.
	if want == c.want && now.Sub(c.sentAt) < reassert {
		return
	}
	if err := c.plug.SetPower(ctx, want == On); err != nil {
		c.failed(err)
		return
	}
	c.want, c.sentAt = want, now
	c.seen(now, want == On)
	level := -1
	if st.Battery != nil {
		level = *st.Battery
	}
	c.log.Info("charger switched", "on", want == On, "battery", level, "battery_at", st.BatteryAt)
}

// watchCharge flags the plug on for a while with fresh readings that do not climb.
func (c *Charger) watchCharge(now time.Time, on bool, st display.Status) {
	level := -1
	if st.Battery != nil {
		level = *st.Battery
	}
	switch {
	case !on:
		c.topAt = time.Time{}
	case c.topAt.IsZero() || level > c.top:
		c.top, c.topAt = level, now
	}
	stalled := on && st.Battery != nil && level < offAt && now.Sub(c.topAt) >= stuck && now.Sub(st.BatteryAt) < fresh
	c.mu.Lock()
	defer c.mu.Unlock()
	if stalled && !c.status.NotCharging {
		c.log.Warn("plug on but the battery is not climbing", "battery", level, "since", c.topAt)
	}
	c.status.NotCharging = stalled
}

func (c *Charger) seen(now time.Time, on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status != nil && c.status.Error != "" {
		c.log.Info("plug reachable again")
	}
	c.status = &Status{On: on, At: now}
}

// failed keeps the last state seen, and warns once per outage.
func (c *Charger) failed(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == nil {
		c.status = &Status{}
	}
	if c.status.Error == "" {
		c.log.Warn("plug unreachable, retrying every minute", "err", err)
	}
	c.status.Error = err.Error()
}
