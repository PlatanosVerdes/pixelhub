package charging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

var t0 = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func TestWant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		level *int
		age   time.Duration
		last  Power
		want  Power
	}{
		{"low", new(3), time.Minute, Off, On},
		{"empty", new(0), 0, Unknown, On},
		{"high", new(7), time.Minute, On, Off},
		{"full", new(9), time.Minute, Unknown, Off},
		{"charging in between keeps charging", new(5), time.Minute, On, On},
		{"resting in between keeps resting", new(4), time.Minute, Off, Off},
		{"in between with no history leaves the plug", new(6), time.Minute, Unknown, Unknown},
		{"stale reading charges", new(9), stale, Off, On},
		{"almost stale still counts", new(9), stale - time.Second, On, Off},
		{"no reading yet leaves the plug", nil, time.Hour, Unknown, Unknown},
		{"no reading yet keeps the last", nil, time.Hour, Off, Off},
		{"no reading for long charges", nil, stale, Off, On},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Want(tt.level, t0, t0.Add(tt.age), tt.last); got != tt.want {
				t.Errorf("Want = %v, want %v", got, tt.want)
			}
		})
	}
}

type fakePlug struct {
	on   bool
	err  error
	sets []bool
}

func (p *fakePlug) Power(context.Context) (bool, error) { return p.on, p.err }
func (p *fakePlug) SetPower(_ context.Context, on bool) error {
	if p.err != nil {
		return p.err
	}
	p.sets = append(p.sets, on)
	p.on = on
	return nil
}

type fakeBattery struct{ st display.Status }

func (b *fakeBattery) Status() display.Status { return b.st }

// step is one pass of the charger at t0+at: what the battery and the plug say, and what it should send.
type step struct {
	at      time.Duration
	level   *int
	plugOn  bool
	plugErr error
	sent    []bool
}

func TestCharger(t *testing.T) {
	t.Parallel()
	down := errors.New("connection refused")
	m := time.Minute
	tests := []struct {
		name  string
		steps []step
	}{
		{"switches on low and off high, once each", []step{
			{at: 0, level: new(5)},
			{at: m, level: new(3), sent: []bool{true}},
			{at: 2 * m, level: new(3), plugOn: true},
			{at: 3 * m, level: new(6), plugOn: true},
			{at: 4 * m, level: new(7), plugOn: true, sent: []bool{false}},
			{at: 5 * m, level: new(8)},
		}},
		{"leaves the plug alone in between at start", []step{
			{at: 0, level: new(5), plugOn: true},
			{at: m, level: new(5)},
		}},
		{"switched by hand is told again at most every ten minutes", []step{
			{at: 0, level: new(2), sent: []bool{true}},
			{at: m, level: new(2)},
			{at: 9 * m, level: new(2)},
			{at: 10 * m, level: new(2), sent: []bool{true}},
			{at: 11 * m, level: new(2)},
		}},
		{"switched by hand to what it wants is left", []step{
			{at: 0, level: new(5), plugOn: true},
			{at: m, level: new(5)},
			{at: 20 * m, level: new(5)},
		}},
		{"no reading since start charges after two hours", []step{
			{at: 0},
			{at: stale - m},
			{at: stale, sent: []bool{true}},
		}},
		{"old reading charges", []step{
			{at: 0, level: new(9), plugOn: true, sent: []bool{false}},
			{at: stale + m, level: new(9), sent: []bool{true}},
		}},
		{"unreachable then back acts on what it missed", []step{
			{at: 0, level: new(8), plugErr: down},
			{at: m, level: new(8), plugErr: down},
			{at: 2 * m, level: new(8), plugOn: true, sent: []bool{false}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plug, battery := &fakePlug{}, &fakeBattery{}
			var now time.Time
			c := &Charger{plug: plug, battery: battery, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: func() time.Time { return now }, started: t0}
			for i, s := range tt.steps {
				now = t0.Add(s.at)
				battery.st = display.Status{}
				if s.level != nil {
					battery.st = display.Status{Battery: s.level, BatteryAt: t0}
				}
				plug.on, plug.err, plug.sets = s.plugOn, s.plugErr, nil
				c.step(context.Background())
				if !slices.Equal(plug.sets, s.sent) {
					t.Errorf("step %d sent %v, want %v", i, plug.sets, s.sent)
				}
				if st := c.Status(); st == nil || (st.Error != "") != (s.plugErr != nil) {
					t.Errorf("step %d status %+v", i, st)
				}
			}
		})
	}
}
