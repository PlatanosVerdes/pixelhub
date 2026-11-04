package hub

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
	"github.com/PlatanosVerdes/pixelhub/internal/sun"
)

const (
	LayerAlert   = "alert"
	LayerScene   = "scene"
	LayerAmbient = "ambient"

	ReasonOverride = "override"
	ReasonSchedule = "schedule"

	tick = time.Second
)

type Sprites interface {
	Get(name string) (display.Content, bool)
}

// Pictures are the channels' ambient pictures, kept outside the state.
type Pictures interface {
	picker
	List(channel string) ([]string, error)
	Add(channel, name string, data []byte) (string, error)
	Remove(channel, file string) error
	RenameChannel(from, to string) error
	DeleteChannel(channel string) error
}

type Alert struct {
	Content  display.Content
	Priority int
	Duration time.Duration
	Source   string
}

type Saver interface {
	Save(state.State) error
}

type Status struct {
	display.Status
	ReleasedUntil  *time.Time `json:"released_until,omitempty"`
	Channel        string     `json:"channel"`
	ChannelReason  string     `json:"channel_reason"`
	OverrideUntil  *time.Time `json:"override_until,omitempty"`
	Layer          string     `json:"layer"`
	Showing        string     `json:"showing"`
	Brightness     int        `json:"brightness"`
	PreviewVersion int        `json:"preview_version"`
}

type channel struct {
	state.Channel
	scenes []scene
}

type scene struct {
	state.Scene
	content display.Content
}

// override pins a channel until a time, or with no time until the schedule moves off `scheduled`.
type override struct {
	channel   string
	until     time.Time
	scheduled string
}

type Hub struct {
	display  display.Display
	store    Saver
	sprites  Sprites
	pictures Pictures
	log      *slog.Logger
	now      func() time.Time
	wake     chan struct{}
	ambient  playlist

	// write serialises every change to the state and the picture folders, so the disk work runs
	// outside mu and never holds up alerts or the display tick.
	write sync.Mutex

	mu             sync.Mutex
	st             state.State
	channels       map[string]channel
	alerts         queue
	override       *override
	releasedUntil  time.Time
	layer          string
	shown          display.Content
	previewVersion int
	ambientStale   bool
}

// New starts from st as loaded. A scene whose sprite file has gone shows the clock rather than
// stopping the service; a change may keep such a scene but not add another.
func New(d display.Display, st state.State, store Saver, sprites Sprites, pictures Pictures, log *slog.Logger) *Hub {
	h := &Hub{
		display: d, store: store, sprites: sprites, pictures: pictures, log: log, now: time.Now,
		wake: make(chan struct{}, 1), ambient: playlist{source: pictures, log: log}, st: st,
	}
	var missing map[string]bool
	if h.channels, missing = h.build(st); len(missing) > 0 {
		h.log.Warn("scenes show the clock instead of missing sprites", "sprites", slices.Sorted(maps.Keys(missing)))
	}
	return h
}

// build resolves each scene's sprite; a missing one shows the clock and is named in missing.
func (h *Hub) build(st state.State) (map[string]channel, map[string]bool) {
	missing := map[string]bool{}
	channels := map[string]channel{}
	for _, ch := range st.Channels {
		c := channel{Channel: ch}
		for _, s := range ch.Scenes {
			content := display.ClockContent()
			if s.Sprite != "" {
				if found, ok := h.sprites.Get(s.Sprite); ok {
					content = found
				} else {
					missing[s.Sprite] = true
				}
			}
			c.scenes = append(c.scenes, scene{Scene: s, content: content})
		}
		channels[ch.Name] = c
	}
	return channels, missing
}

// Update applies change to a copy of the state, checks it, saves it and makes it live.
func (h *Hub) Update(change func(*state.State) error) (state.State, error) {
	h.write.Lock()
	defer h.write.Unlock()
	next, channels, err := h.prepare(change)
	if err != nil {
		return state.State{}, err
	}
	return h.commit(next, channels, "", "")
}

// AddChannel creates a channel; pictures left behind by an old channel of that name go first.
func (h *Hub) AddChannel(c state.Channel) (state.State, error) {
	h.write.Lock()
	defer h.write.Unlock()
	next, channels, err := h.prepare(func(s *state.State) error { return s.AddChannel(c) })
	if err != nil {
		return state.State{}, err
	}
	if err := h.pictures.DeleteChannel(c.Name); err != nil {
		return state.State{}, err
	}
	return h.commit(next, channels, "", "")
}

// ReplaceChannel updates a channel; a new name also moves its pictures and any override on it.
func (h *Hub) ReplaceChannel(name string, c state.Channel) (state.State, error) {
	h.write.Lock()
	defer h.write.Unlock()
	next, channels, err := h.prepare(func(s *state.State) error { return s.ReplaceChannel(name, c) })
	if err != nil {
		return state.State{}, err
	}
	to := cmp.Or(c.Name, name)
	if to == name {
		return h.commit(next, channels, "", "")
	}
	if err := h.pictures.RenameChannel(name, to); err != nil {
		return state.State{}, err
	}
	st, err := h.commit(next, channels, name, to)
	if err != nil {
		if undo := h.pictures.RenameChannel(to, name); undo != nil {
			h.log.Error("pictures left under the new channel name", "from", name, "to", to, "err", undo)
		}
	}
	return st, err
}

// DeleteChannel removes a channel, its schedule slots and its pictures.
func (h *Hub) DeleteChannel(name string) (state.State, error) {
	h.write.Lock()
	defer h.write.Unlock()
	next, channels, err := h.prepare(func(s *state.State) error { return s.DeleteChannel(name) })
	if err != nil {
		return state.State{}, err
	}
	st, err := h.commit(next, channels, "", "")
	if err != nil {
		return st, err
	}
	if err := h.pictures.DeleteChannel(name); err != nil {
		h.log.Error("channel deleted but not its pictures", "channel", name, "err", err)
	}
	return st, nil
}

func (h *Hub) ListPictures(channel string) ([]string, error) {
	if err := h.hasChannel(channel); err != nil {
		return nil, err
	}
	return h.pictures.List(channel)
}

func (h *Hub) AddPicture(channel, name string, data []byte) (string, error) {
	h.write.Lock()
	defer h.write.Unlock()
	if err := h.hasChannel(channel); err != nil {
		return "", err
	}
	saved, err := h.pictures.Add(channel, name, data)
	if err == nil {
		h.refreshAmbient()
	}
	return saved, err
}

func (h *Hub) RemovePicture(channel, file string) error {
	h.write.Lock()
	defer h.write.Unlock()
	if err := h.hasChannel(channel); err != nil {
		return err
	}
	err := h.pictures.Remove(channel, file)
	if err == nil {
		h.refreshAmbient()
	}
	return err
}

func (h *Hub) hasChannel(name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.channels[name]; !ok {
		return fmt.Errorf("%w: no channel %q", state.ErrNotFound, name)
	}
	return nil
}

// prepare needs h.write held: the copy it changes must still be current when commit swaps it in.
func (h *Hub) prepare(change func(*state.State) error) (state.State, map[string]channel, error) {
	h.mu.Lock()
	next := h.st.Clone()
	h.mu.Unlock()
	if err := change(&next); err != nil {
		return state.State{}, nil, err
	}
	next = next.Clone()
	if err := next.Validate(); err != nil {
		return state.State{}, nil, fmt.Errorf("%w: %w", state.ErrInvalid, err)
	}
	channels, missing := h.build(next)
	h.mu.Lock()
	_, missingBefore := h.build(h.st)
	h.mu.Unlock()
	for name := range missing {
		if !missingBefore[name] {
			return state.State{}, nil, fmt.Errorf("%w: no sprite named %q", state.ErrInvalid, name)
		}
	}
	return next, channels, nil
}

// commit saves next and makes it live; an override follows a rename from -> to and ends with its channel.
func (h *Hub) commit(next state.State, channels map[string]channel, from, to string) (state.State, error) {
	if err := h.store.Save(next); err != nil {
		return state.State{}, err
	}
	h.mu.Lock()
	h.st, h.channels = next, channels
	if o := h.override; o != nil {
		if o.channel == from {
			o.channel = to
		}
		if o.scheduled == from {
			o.scheduled = to
		}
		if _, ok := channels[o.channel]; !ok {
			h.override = nil
		}
	}
	h.mu.Unlock()
	h.poke()
	return next.Clone(), nil
}

func (h *Hub) refreshAmbient() {
	h.mu.Lock()
	h.ambientStale = true
	h.mu.Unlock()
	h.poke()
}

func (h *Hub) State() state.State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.st.Clone()
}

// Preview is what the display should show now; its version changes whenever that does.
func (h *Hub) Preview() (display.Content, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shown, h.previewVersion
}

func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		h.update()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-h.wake:
		}
	}
}

// Notify queues the alert when the active channel shows its source, and returns that channel.
func (h *Hub) Notify(a Alert) (string, bool) {
	h.mu.Lock()
	now := h.now()
	name, _ := h.activeChannel(now)
	accepted := h.channels[name].Accepts(a.Source)
	if accepted {
		h.alerts.push(a, now)
	}
	h.mu.Unlock()
	if accepted {
		h.poke()
	}
	return name, accepted
}

// SetChannel pins a channel for d, or until the schedule changes when d is zero.
func (h *Hub) SetChannel(name string, d time.Duration) error {
	h.mu.Lock()
	if _, ok := h.channels[name]; !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: no channel %q", state.ErrNotFound, name)
	}
	now := h.now()
	o := &override{channel: name, scheduled: h.st.ScheduledChannel(now)}
	if d > 0 {
		o.until = now.Add(d)
	}
	h.override = o
	h.mu.Unlock()
	h.log.Info("channel set", "channel", name, "for", d.String())
	h.poke()
	return nil
}

func (h *Hub) ClearChannel() {
	h.mu.Lock()
	h.override = nil
	h.mu.Unlock()
	h.log.Info("channel override cleared")
	h.poke()
}

func (h *Hub) Release() time.Time {
	h.mu.Lock()
	h.releasedUntil = h.now().Add(time.Duration(h.st.ReleaseFor))
	until := h.releasedUntil
	h.mu.Unlock()
	h.display.Release()
	h.log.Info("display released", "until", until)
	return until
}

func (h *Hub) Claim() {
	h.mu.Lock()
	h.releasedUntil = time.Time{}
	h.mu.Unlock()
	h.display.Claim()
	h.log.Info("display claimed")
}

func (h *Hub) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := Status{
		Status: h.display.Status(), Layer: h.layer, Showing: h.shown.Name, Brightness: h.shown.Brightness,
		PreviewVersion: h.previewVersion,
	}
	s.Channel, s.ChannelReason = h.activeChannel(h.now())
	if h.override != nil && !h.override.until.IsZero() {
		s.OverrideUntil = new(h.override.until)
	}
	if !h.releasedUntil.IsZero() {
		s.ReleasedUntil = new(h.releasedUntil)
	}
	return s
}

func (h *Hub) poke() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// activeChannel needs h.mu held; it drops an override that has run out.
func (h *Hub) activeChannel(now time.Time) (name, reason string) {
	scheduled := h.st.ScheduledChannel(now)
	if o := h.override; o != nil {
		expired := scheduled != o.scheduled
		if !o.until.IsZero() {
			expired = !now.Before(o.until)
		}
		if !expired {
			return o.channel, ReasonOverride
		}
		h.override = nil
	}
	return scheduled, ReasonSchedule
}

func (h *Hub) update() {
	now := h.now()
	h.mu.Lock()
	claim := !h.releasedUntil.IsZero() && !now.Before(h.releasedUntil)
	if claim {
		h.releasedUntil = time.Time{}
	}
	name, _ := h.activeChannel(now)
	ch := h.channels[name]
	level := h.sunLevel(now)
	layer, content, found := h.aboveAmbient(now, ch, level)
	stale := h.ambientStale && !found
	if stale {
		h.ambientStale = false
	}
	h.mu.Unlock()

	if !found {
		layer, content = LayerAmbient, h.ambient.at(now, name, ch.Ambient, stale).WithBrightness(level)
	}

	h.mu.Lock()
	changed := !content.SamePicture(h.shown) || content.Brightness != h.shown.Brightness
	h.layer, h.shown = layer, content
	if changed {
		h.previewVersion++
	}
	h.mu.Unlock()

	if claim {
		h.display.Claim()
		h.log.Info("display claimed after the release time")
	}
	if changed {
		h.log.Info("showing", "channel", name, "layer", layer, "content", content.Name, "brightness", content.Brightness)
		h.display.Show(content)
	}
}

// aboveAmbient returns the alert or scene to show, if any; the ambient layer is left to the caller
// so pictures are only read from disk when they will be seen, and never under h.mu.
func (h *Hub) aboveAmbient(now time.Time, ch channel, level int) (string, display.Content, bool) {
	if a, ok := h.alerts.head(now); ok {
		return LayerAlert, a.Content.WithBrightness(level), true
	}
	for _, s := range ch.scenes {
		if s.ActiveAt(now) {
			if s.Brightness != nil {
				level = int(*s.Brightness)
			}
			return LayerScene, s.content.WithBrightness(level), true
		}
	}
	return "", display.Content{}, false
}

func (h *Hub) sunLevel(now time.Time) int {
	switch sun.PhaseAt(now, h.st.Location.Latitude, h.st.Location.Longitude) {
	case sun.Day:
		return h.st.Brightness.Day
	case sun.Dusk:
		return h.st.Brightness.Dusk
	default:
		return h.st.Brightness.Night
	}
}
