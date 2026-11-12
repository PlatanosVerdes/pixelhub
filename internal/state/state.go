package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

const AllSources = "*"

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)

// State is everything the owner can change; it lives in state.json.
type State struct {
	Location       Location  `json:"location"`
	Brightness     Levels    `json:"brightness"`
	ReleaseFor     Duration  `json:"release_for"`
	DefaultChannel string    `json:"default_channel"`
	Channels       []Channel `json:"channels"`
	Schedule       []Slot    `json:"schedule"`
	// Mode, when set, is one of the display's own modes picked by hand; it shows over scenes and
	// ambient until cleared, alerts still go on top.
	Mode display.View `json:"mode,omitzero"`
	// Music shows what a linked music account plays, when it plays: its cover, or View instead.
	Music Music `json:"music,omitzero"`
	// Clock is how the clock looks wherever pixelhub shows it, so the display's stored look never leaks in.
	Clock display.View `json:"clock,omitzero"`
	// Talk is how Mosko speaks.
	Talk Talk `json:"talk,omitzero"`
	// PowerOffMin is the display's auto power-off in minutes; 0, the default, keeps it on for good.
	PowerOffMin int `json:"power_off_min,omitempty"`
	// Volume is the speaker's volume in percent; unset leaves the display's own.
	Volume *int `json:"volume,omitempty"`
}

// Talk tunes Mosko. LagMS is how much later the speaker sounds than the display moves, measured by
// ear; Pitch is the voice's pitch in percent, 100 as recorded or spoken.
type Talk struct {
	LagMS *int `json:"lag_ms,omitempty"`
	Pitch int  `json:"pitch,omitempty"`
	// Effect is one of talk.Effects.
	Effect string `json:"effect,omitempty"`
}

// defaultLag is what the Evo's speaker took over the raspi's Bluetooth by ear: PipeWire alone reports 140 ms.
const defaultLag = 250 * time.Millisecond

// Lag is LagMS, defaultLag when unset.
func (t Talk) Lag() time.Duration {
	if t.LagMS == nil {
		return defaultLag
	}
	return time.Duration(*t.LagMS) * time.Millisecond
}

// PitchRatio is Pitch as a factor, 1 when unset.
func (t Talk) PitchRatio() float64 {
	if t.Pitch == 0 {
		return 1
	}
	return float64(t.Pitch) / 100
}

// maxPowerOff is the longest auto power-off the Divoom app offers, 12 hours.
const maxPowerOff = 720

var defaultClock = display.View{Mode: display.ModeClock, Color: "#ff1e28"}

// ClockView is the clock to show: Clock, or a plain red one until it is set.
func (s State) ClockView() display.View {
	if s.Clock.IsZero() {
		return defaultClock
	}
	return s.Clock
}

type Music struct {
	Enabled bool         `json:"enabled"`
	View    display.View `json:"view,omitzero"`
	// Devices, when set, limits the music layer to what plays on these devices.
	Devices []string `json:"devices,omitempty"`
}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	// Name is the place as the user picked it, only for showing.
	Name string `json:"name,omitempty"`
}

type Levels struct {
	Day   int `json:"day"`
	Dusk  int `json:"dusk"`
	Night int `json:"night"`
}

type Channel struct {
	Name    string   `json:"name"`
	Sources []string `json:"sources"`
	Ambient Ambient  `json:"ambient"`
	Scenes  []Scene  `json:"scenes"`
}

// Ambient alternates pictures with the native clock; a zero length skips that part.
type Ambient struct {
	Pictures Duration `json:"pictures"`
	Clock    Duration `json:"clock"`
	// List, when set, is the list of pictures to show instead of the channel's own folder.
	List string `json:"list,omitempty"`
}

// Window is a time range on a set of weekdays.
type Window struct {
	Days Days  `json:"days"`
	From Clock `json:"from"`
	To   Clock `json:"to"`
}

type Scene struct {
	Name string `json:"name"`
	Window
	Sprite string       `json:"sprite,omitempty"`
	View   display.View `json:"view,omitzero"`
	// List shows the pictures of a list in turn, with the channel's own timing, during the window.
	List       string `json:"list,omitempty"`
	Brightness *Level `json:"brightness,omitempty"`
}

type Slot struct {
	Channel string `json:"channel"`
	Window
}

// Defaults is a fresh install: one channel that shows the clock and every alert.
func Defaults() State {
	return State{
		Location:       Location{Latitude: 51.4769, Longitude: -0.0005},
		Brightness:     Levels{Day: 80, Dusk: 40, Night: 10},
		ReleaseFor:     Duration(3 * time.Hour),
		DefaultChannel: "clock",
		Channels: []Channel{{
			Name:    "clock",
			Sources: []string{AllSources},
			Ambient: Ambient{Clock: Duration(time.Minute)},
			Scenes:  []Scene{},
		}},
		Schedule: []Slot{},
	}
}

// NewAmbient is the playlist of a channel created without one.
func NewAmbient() Ambient {
	return Ambient{Pictures: Duration(2 * time.Minute), Clock: Duration(3 * time.Minute)}
}

func (s State) Channel(name string) (Channel, bool) {
	i := s.index(name)
	if i < 0 {
		return Channel{}, false
	}
	return s.Channels[i], true
}

func (s State) index(name string) int {
	return slices.IndexFunc(s.Channels, func(c Channel) bool { return c.Name == name })
}

// AddChannel gives a channel without an ambient playlist the usual one.
func (s *State) AddChannel(c Channel) error {
	if c.Ambient == (Ambient{}) {
		c.Ambient = NewAmbient()
	}
	if s.index(c.Name) >= 0 {
		return fmt.Errorf("%w: channel %q already exists", ErrConflict, c.Name)
	}
	s.Channels = append(s.Channels, c)
	return nil
}

// ReplaceChannel updates channel name with c; a new name in c renames it everywhere it is used.
// An empty name or ambient in c keeps the channel's own.
func (s *State) ReplaceChannel(name string, c Channel) error {
	if c.Name == "" {
		c.Name = name
	}
	i := s.index(name)
	if i < 0 {
		return fmt.Errorf("%w: no channel %q", ErrNotFound, name)
	}
	if c.Name != name {
		if s.index(c.Name) >= 0 {
			return fmt.Errorf("%w: channel %q already exists", ErrConflict, c.Name)
		}
		for j := range s.Schedule {
			if s.Schedule[j].Channel == name {
				s.Schedule[j].Channel = c.Name
			}
		}
		if s.DefaultChannel == name {
			s.DefaultChannel = c.Name
		}
	}
	if c.Ambient == (Ambient{}) {
		c.Ambient = s.Channels[i].Ambient
	}
	s.Channels[i] = c
	return nil
}

// DeleteChannel also drops the channel's schedule slots.
func (s *State) DeleteChannel(name string) error {
	i := s.index(name)
	switch {
	case i < 0:
		return fmt.Errorf("%w: no channel %q", ErrNotFound, name)
	case len(s.Channels) == 1:
		return fmt.Errorf("%w: %q is the last channel", ErrConflict, name)
	case s.DefaultChannel == name:
		return fmt.Errorf("%w: %q is the default channel; pick another default first", ErrConflict, name)
	}
	s.Channels = slices.Delete(s.Channels, i, i+1)
	s.Schedule = slices.DeleteFunc(s.Schedule, func(slot Slot) bool { return slot.Channel == name })
	return nil
}

// ApplySettings merges a JSON object of settings into s. Decoding into the live fields keeps
// whatever the object leaves out, nested fields included.
func (s *State) ApplySettings(data []byte) error {
	settings := struct {
		Location       *Location `json:"location"`
		Brightness     *Levels   `json:"brightness"`
		ReleaseFor     *Duration `json:"release_for"`
		DefaultChannel *string   `json:"default_channel"`
		// Music is replaced whole: leaving out its view means the cover.
		Music       *Music        `json:"music"`
		Clock       *display.View `json:"clock"`
		Talk        *Talk         `json:"talk"`
		PowerOffMin *int          `json:"power_off_min"`
		Volume      **int         `json:"volume"`
	}{Volume: &s.Volume, PowerOffMin: &s.PowerOffMin, Talk: &s.Talk, Location: &s.Location, Brightness: &s.Brightness, ReleaseFor: &s.ReleaseFor, DefaultChannel: &s.DefaultChannel}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&settings); err != nil {
		return fmt.Errorf("%w: settings: %w", ErrInvalid, err)
	}
	if settings.Music != nil {
		s.Music = *settings.Music
	}
	if settings.Clock != nil {
		s.Clock = *settings.Clock
	}
	return nil
}

var channelName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Validate returns every problem at once, so the API can answer with all of them.
func (s State) Validate() error {
	var errs []error
	if s.Location.Latitude < -90 || s.Location.Latitude > 90 || s.Location.Longitude < -180 || s.Location.Longitude > 180 {
		errs = append(errs, errors.New("location: latitude or longitude out of range"))
	}
	for name, v := range map[string]int{"day": s.Brightness.Day, "dusk": s.Brightness.Dusk, "night": s.Brightness.Night} {
		if v < 0 || v > 100 {
			errs = append(errs, fmt.Errorf("brightness.%s: %d is outside 0-100", name, v))
		}
	}
	if lag := s.Talk.Lag(); lag < -2*time.Second || lag > 2*time.Second {
		errs = append(errs, fmt.Errorf("talk.lag_ms: %d is outside -2000 to 2000", lag.Milliseconds()))
	}
	if s.Volume != nil && (*s.Volume < 0 || *s.Volume > 100) {
		errs = append(errs, fmt.Errorf("volume: %d is outside 0-100", *s.Volume))
	}
	if s.PowerOffMin < 0 || s.PowerOffMin > maxPowerOff {
		errs = append(errs, fmt.Errorf("power_off_min: %d is outside 0-%d", s.PowerOffMin, maxPowerOff))
	}
	if s.Talk.Pitch != 0 && (s.Talk.Pitch < 50 || s.Talk.Pitch > 200) {
		errs = append(errs, fmt.Errorf("talk.pitch: %d is outside 50-200", s.Talk.Pitch))
	}
	if _, ok := talk.Effects[s.Talk.Effect]; !ok {
		errs = append(errs, fmt.Errorf("talk.effect: no effect %q", s.Talk.Effect))
	}
	if time.Duration(s.ReleaseFor) < time.Minute {
		errs = append(errs, fmt.Errorf("release_for: %s is under a minute", time.Duration(s.ReleaseFor)))
	}
	if len(s.Channels) == 0 {
		errs = append(errs, errors.New("channels: there must be at least one"))
	}
	if s.index(s.DefaultChannel) < 0 {
		errs = append(errs, fmt.Errorf("default_channel: %q is not a channel", s.DefaultChannel))
	}
	seen := map[string]bool{}
	for _, c := range s.Channels {
		if seen[c.Name] {
			errs = append(errs, fmt.Errorf("channel %q appears twice", c.Name))
		}
		seen[c.Name] = true
		errs = append(errs, c.validate()...)
	}
	if !s.Mode.IsZero() {
		if err := s.Mode.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("mode: %w", err))
		}
	}
	if !s.Clock.IsZero() {
		if err := s.Clock.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("clock: %w", err))
		} else if s.Clock.Mode != display.ModeClock || s.Clock.Color == "" {
			errs = append(errs, errors.New("clock: must be the clock mode with a colour"))
		}
	}
	if !s.Music.View.IsZero() {
		if err := s.Music.View.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("music.view: %w", err))
		}
	}
	for i, slot := range s.Schedule {
		if s.index(slot.Channel) < 0 {
			errs = append(errs, fmt.Errorf("schedule[%d]: %q is not a channel", i, slot.Channel))
		}
		if slot.From == slot.To {
			errs = append(errs, fmt.Errorf("schedule[%d]: from and to are the same", i))
		}
	}
	return errors.Join(errs...)
}

func (c Channel) validate() []error {
	var errs []error
	if !channelName.MatchString(c.Name) {
		errs = append(errs, fmt.Errorf("channel name %q: use 1-32 lowercase letters, digits, - or _", c.Name))
	}
	for _, src := range c.Sources {
		if src == "" {
			errs = append(errs, fmt.Errorf("channel %s: empty source", c.Name))
		}
	}
	pictures, clock := time.Duration(c.Ambient.Pictures), time.Duration(c.Ambient.Clock)
	switch {
	case pictures < 0 || clock < 0 || pictures+clock == 0:
		errs = append(errs, fmt.Errorf("channel %s: ambient needs pictures or clock", c.Name))
	case pictures > 0 && pictures+clock < 5*time.Second:
		errs = append(errs, fmt.Errorf("channel %s: ambient pictures and clock must add up to 5s or more", c.Name))
	}
	for i, sc := range c.Scenes {
		if sc.Name == "" {
			errs = append(errs, fmt.Errorf("channel %s: scenes[%d] has no name", c.Name, i))
		}
		if sc.From == sc.To {
			errs = append(errs, fmt.Errorf("channel %s: scene %q: from and to are the same", c.Name, sc.Name))
		}
		if n := btoi(sc.Sprite != "") + btoi(!sc.View.IsZero()) + btoi(sc.List != ""); n > 1 {
			errs = append(errs, fmt.Errorf("channel %s: scene %q: give a sprite, a view or a list, not more than one", c.Name, sc.Name))
		}
		if !sc.View.IsZero() {
			if err := sc.View.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("channel %s: scene %q: %w", c.Name, sc.Name, err))
			}
		}
	}
	return errs
}

// Accepts reports whether alerts from source show on this channel; an alert without a source needs "*".
func (c Channel) Accepts(source string) bool {
	return slices.Contains(c.Sources, AllSources) || (source != "" && slices.Contains(c.Sources, source))
}

// ScheduledChannel is the first schedule slot holding t, or the default channel.
func (s State) ScheduledChannel(t time.Time) string {
	for _, slot := range s.Schedule {
		if slot.ActiveAt(t) {
			return slot.Channel
		}
	}
	return s.DefaultChannel
}

// ActiveAt reports whether t falls in the window. A range past midnight belongs to the day it starts on.
func (w Window) ActiveAt(t time.Time) bool {
	now := Clock(t.Hour()*60 + t.Minute())
	if w.From < w.To {
		return w.Days.Has(t.Weekday()) && now >= w.From && now < w.To
	}
	if now >= w.From {
		return w.Days.Has(t.Weekday())
	}
	return now < w.To && w.Days.Has(t.AddDate(0, 0, -1).Weekday())
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
