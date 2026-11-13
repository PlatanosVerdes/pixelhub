package hub

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/charging"
	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
	"github.com/PlatanosVerdes/pixelhub/internal/sun"
)

const (
	LayerAlert   = "alert"
	LayerMode    = "mode"
	LayerMusic   = "music"
	LayerScene   = "scene"
	LayerAmbient = "ambient"

	ReasonOverride = "override"
	ReasonSchedule = "schedule"

	tick = time.Second
	// A tool left at zero this long gives the screen back.
	idleTool    = time.Minute
	shownSource = "shown"
	// A light reading older than this no longer sets the brightness, the sun does again.
	lightFresh = 2 * time.Minute
	// A sound while the display is off shows the time this dim, this long, at most this often.
	wakeLevel, wakeFor, wakeEvery = 5, 10 * time.Second, 30 * time.Second
	// batteryPriority is above anything picked by hand: a dying display is worth knowing.
	batteryPriority = 8
	// showPriority puts something picked by hand over routine alerts but under urgent ones.
	showPriority = 5
	// talkPriority speaks over what was picked by hand, which would otherwise hold it back for minutes.
	talkPriority = showPriority + 1
	talkSource   = "talk"
)

type Sprites interface {
	Get(name string) (display.Content, bool)
}

// Pictures are the channels' ambient pictures, kept outside the state.
type Pictures interface {
	picker
	List(channel string) ([]string, error)
	First(channel, file string) (image.Image, error)
	Add(channel, name string, data []byte) (string, error)
	Remove(channel, file string) error
	RenameChannel(from, to string) error
	DeleteChannel(channel string) error
}

type Weather interface {
	Current() display.Weather
}

// Music is what a linked music account plays now, if anything, and on which device.
type Music interface {
	Content() (c display.Content, device string, playing bool)
}

type Alert struct {
	Content  display.Content
	Priority int
	Duration time.Duration
	Source   string
	// Brightness, when set, is used instead of the one for the time of day.
	Brightness int
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
	// Plug is the charger's plug, when pixelhub switches it.
	Plug *charging.Status `json:"plug,omitempty"`
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
	// Music, when set before Run, adds the music layer under a picked mode.
	Music Music
	// Weather, when set before Run, feeds the clock's weather and temperature.
	Weather Weather
	// Lists, when set before Run, lets ambient rules and scenes show a list of pictures.
	Lists ListPicker
	// Plug, when set, adds the charger's plug to the status.
	Plug interface{ Status() *charging.Status }

	// write serialises every change to the state and the picture folders, so the disk work runs
	// outside mu and never holds up alerts or the display tick.
	write sync.Mutex

	// The room as the bridge's sensors or the API report it; guarded by mu.
	light               int
	lightAt, soundAt    time.Time
	wokeAt, soundSeenAt time.Time

	// battery tells when to show the battery; only update uses it.
	battery batteryWatch
	// notChargingAt is when the not-charging picture last showed, zero while charging is fine.
	notChargingAt time.Time

	// idle is when the picked tool was first seen at zero, and which tool that was.
	idle     time.Time
	idleView display.View

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
			if !s.View.IsZero() {
				content = s.View.Content()
			} else if s.Sprite != "" {
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
	return h.apply(change, true)
}

func (h *Hub) apply(change func(*state.State) error, byHand bool) (state.State, error) {
	h.write.Lock()
	defer h.write.Unlock()
	next, channels, err := h.prepare(change)
	if err != nil {
		return state.State{}, err
	}
	return h.commitBy(next, channels, "", "", byHand)
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

func (h *Hub) PictureImage(channel, file string) (image.Image, error) {
	if err := h.hasChannel(channel); err != nil {
		return nil, err
	}
	return h.pictures.First(channel, file)
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
	return h.commitBy(next, channels, from, to, true)
}

func (h *Hub) commitBy(next state.State, channels map[string]channel, from, to string, byHand bool) (state.State, error) {
	if err := h.store.Save(next); err != nil {
		return state.State{}, err
	}
	h.mu.Lock()
	// Picking a mode or changing a channel by hand takes over from what was shown by hand.
	if byHand && (!reflect.DeepEqual(next.Mode, h.st.Mode) || !reflect.DeepEqual(next.Channels, h.st.Channels)) {
		h.alerts.drop(shownSource, h.now())
	}
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

// Show puts c on screen for d like an alert, whatever sources the active channel takes.
func (h *Hub) Show(c display.Content, d time.Duration) {
	h.mu.Lock()
	// What was shown by hand gives way at once to the next thing shown, rather than queue behind it.
	now := h.now()
	h.alerts.drop(shownSource, now)
	h.alerts.push(Alert{Content: c, Priority: showPriority, Duration: d, Source: shownSource}, now)
	h.mu.Unlock()
	h.poke()
}

// Unshow takes off what was shown by hand, if it is still on and its key starts with prefix.
func (h *Hub) Unshow(prefix string) {
	h.mu.Lock()
	h.alerts.dropFunc(func(a Alert) bool { return a.Source == shownSource && strings.HasPrefix(a.Content.Key, prefix) }, h.now())
	h.mu.Unlock()
	h.poke()
}

// ErrLent is a request the display cannot take while the phone app has it.
var ErrLent = errors.New("the display is lent to the phone app")

// Say puts a talking clip on screen for d, cutting short the one before.
func (h *Hub) Say(c display.Content, d time.Duration) error {
	h.mu.Lock()
	if !h.releasedUntil.IsZero() {
		h.mu.Unlock()
		return ErrLent
	}
	now := h.now()
	h.alerts.drop(talkSource, now)
	h.alerts.push(Alert{Content: c, Priority: talkPriority, Duration: d, Source: talkSource}, now)
	h.mu.Unlock()
	h.poke()
	return nil
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
	h.alerts.drop(shownSource, now)
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

func (h *Hub) Retry() {
	h.display.Retry()
}

// Chime plays a short sound on the display, unless it is lent to the phone app.
func (h *Hub) Chime() {
	h.mu.Lock()
	released := !h.releasedUntil.IsZero()
	h.mu.Unlock()
	if !released {
		h.display.Chime()
	}
}

func (h *Hub) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := Status{
		Status: h.display.Status(), Layer: h.layer, Showing: h.shown.Name, Brightness: h.shown.Brightness,
		PreviewVersion: h.previewVersion,
	}
	if h.Plug != nil {
		s.Plug = h.Plug.Status()
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
	h.watchBattery(now)
	h.pullSensors()
	h.wakeOnSound(now)
	if h.toolDone(now) {
		if _, err := h.apply(func(s *state.State) error { s.Mode = display.View{}; return nil }, false); err != nil {
			h.log.Warn("idle tool not cleared", "err", err)
		}
		h.log.Info("tool left at zero, back to automatic")
	}
	h.mu.Lock()
	claim := !h.releasedUntil.IsZero() && !now.Before(h.releasedUntil)
	if claim {
		h.releasedUntil = time.Time{}
	}
	name, _ := h.activeChannel(now)
	ch := h.channels[name]
	level := h.sunLevel(now)
	layer, content, found, listScene := h.aboveAmbient(now, ch, level)
	stale := h.ambientStale && !found
	if stale {
		h.ambientStale = false
	}
	h.mu.Unlock()

	if !found {
		rule := ch.Ambient
		layer = LayerAmbient
		if listScene != nil {
			// A scene with a list plays it like ambient does, with the channel's timing.
			rule.List, layer = listScene.List, LayerScene
			if rule.Pictures == 0 {
				rule.Pictures = state.Duration(time.Minute)
			}
			if listScene.Brightness != nil {
				level = int(*listScene.Brightness)
			}
		}
		content = h.ambient.at(now, name, rule, stale, h.Lists).WithBrightness(level)
	}

	var weather display.Weather
	if h.Weather != nil {
		weather = h.Weather.Current()
	}

	h.mu.Lock()
	if content.Kind == display.Clock {
		clock := h.st.ClockView().Content().WithBrightness(content.Brightness)
		clock.Name = content.Name
		content = clock
	}
	// A clock without a colour of its own is the clock: it takes the one look set for it.
	if content.Kind == display.Native && content.View.Mode == display.ModeClock && content.View.Color == "" {
		content.View = h.st.ClockView()
	}
	if content.Kind == display.Native && content.View.Mode == display.ModeClock {
		content.Weather = weather
	}
	changed := !content.SamePicture(h.shown) || content.Brightness != h.shown.Brightness
	powerOff, volume := h.st.PowerOffMin, h.st.Volume
	h.layer, h.shown = layer, content
	if changed {
		h.previewVersion++
	}
	h.mu.Unlock()

	h.display.SetPowerOff(powerOff)
	if volume != nil {
		h.display.SetVolume(*volume)
	}
	if claim {
		h.display.Claim()
		h.log.Info("display claimed after the release time")
	}
	if changed {
		h.log.Info("showing", "channel", name, "layer", layer, "content", content.Name, "brightness", content.Brightness)
		h.display.Show(content)
	}
}

// aboveAmbient returns the alert, mode, music or scene to show, if any, or the scene whose list ambient should play;
// the ambient layer is left to the caller
// so pictures are only read from disk when they will be seen, and never under h.mu.
func (h *Hub) aboveAmbient(now time.Time, ch channel, level int) (string, display.Content, bool, *state.Scene) {
	if a, ok := h.alerts.head(now); ok {
		if a.Brightness > 0 {
			level = a.Brightness
		}
		return LayerAlert, a.Content.WithBrightness(level), true, nil
	}
	if !h.st.Mode.IsZero() {
		return LayerMode, h.st.Mode.Content().WithBrightness(level), true, nil
	}
	if h.st.Music.Enabled && h.Music != nil {
		c, device, ok := h.Music.Content()
		if ok && (len(h.st.Music.Devices) == 0 || slices.Contains(h.st.Music.Devices, device)) {
			if !h.st.Music.View.IsZero() {
				c = h.st.Music.View.Content()
			}
			return LayerMusic, c.WithBrightness(level), true, nil
		}
	}
	for _, s := range ch.scenes {
		if s.ActiveAt(now) {
			if s.List != "" {
				return "", display.Content{}, false, &s.Scene
			}
			if s.Brightness != nil {
				level = int(*s.Brightness)
			}
			return LayerScene, s.content.WithBrightness(level), true, nil
		}
	}
	return "", display.Content{}, false, nil
}

func (h *Hub) sunLevel(now time.Time) int {
	if !h.lightAt.IsZero() && now.Sub(h.lightAt) < lightFresh {
		// with a light sensor, the room decides between the night and the day level
		b := h.st.Brightness
		return b.Night + (b.Day-b.Night)*h.light/255
	}
	switch sun.PhaseAt(now, h.st.Location.Latitude, h.st.Location.Longitude) {
	case sun.Day:
		return h.st.Brightness.Day
	case sun.Dusk:
		return h.st.Brightness.Dusk
	default:
		return h.st.Brightness.Night
	}
}

// toolDone reports a picked tool that has sat at zero for idleTool: a stopped stopwatch or noise
// meter at zero, or a countdown that ran out. Anything paused with time on it stays.
func (h *Hub) toolDone(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.st.Mode
	var since time.Time
	switch {
	case !m.Mode.Tool():
	case m.Mode == display.ModeCountdown && m.Running && m.Since != 0:
		since = time.Unix(m.Since+int64(m.Seconds), 0)
	case m.Mode != display.ModeCountdown && !m.Running && m.Seconds == 0:
		if h.idle.IsZero() || h.idleView != m {
			h.idle, h.idleView = now, m
		}
		since = h.idle
	}
	if since.IsZero() || now.Before(since) {
		if since.IsZero() {
			h.idle = time.Time{}
		}
		return false
	}
	return now.Sub(since) >= idleTool
}

// Sense records what the room's sensors report: light from 0 (dark) to 255, or a sound.
func (h *Hub) Sense(light *int, sound bool) {
	h.mu.Lock()
	now := h.now()
	if light != nil {
		h.light, h.lightAt = max(0, min(255, *light)), now
	}
	if sound {
		h.soundAt = now
	}
	h.mu.Unlock()
	h.poke()
}

// pullSensors takes newer readings from the bridge, which may carry the sensors itself.
func (h *Hub) pullSensors() {
	st := h.display.Status()
	h.mu.Lock()
	defer h.mu.Unlock()
	if st.Light != nil && st.LightAt.After(h.lightAt) {
		h.light, h.lightAt = *st.Light, st.LightAt
	}
	if st.SoundAt.After(h.soundAt) {
		h.soundAt = st.SoundAt
	}
}

// wakeOnSound shows the time, dim, when a sound comes while a scene keeps the display off.
func (h *Hub) wakeOnSound(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.soundAt.IsZero() || !h.soundAt.After(h.soundSeenAt) {
		return
	}
	h.soundSeenAt = h.soundAt
	if now.Sub(h.wokeAt) < wakeEvery || !h.dark(now) {
		return
	}
	h.wokeAt = now
	c := sprite.SleepyClock(now, h.st.ClockView().RGB())
	h.alerts.push(Alert{Content: c, Priority: 1, Duration: wakeFor, Source: "sound", Brightness: wakeLevel}, now)
	h.log.Info("woken by a sound")
}

// dark reports a scene keeping the display off right now; it needs h.mu held.
func (h *Hub) dark(now time.Time) bool {
	name, _ := h.activeChannel(now)
	for _, s := range h.channels[name].scenes {
		if s.ActiveAt(now) {
			return s.Brightness != nil && *s.Brightness == 0
		}
	}
	return false
}
