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
	Sprite     string       `json:"sprite,omitempty"`
	View       display.View `json:"view,omitzero"`
	Brightness *Level       `json:"brightness,omitempty"`
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
		Music *Music `json:"music"`
	}{Location: &s.Location, Brightness: &s.Brightness, ReleaseFor: &s.ReleaseFor, DefaultChannel: &s.DefaultChannel}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&settings); err != nil {
		return fmt.Errorf("%w: settings: %w", ErrInvalid, err)
	}
	if settings.Music != nil {
		s.Music = *settings.Music
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
	if pictures < 0 || clock < 0 || pictures+clock < 10*time.Second {
		errs = append(errs, fmt.Errorf("channel %s: ambient pictures and clock must add up to 10s or more", c.Name))
	}
	for i, sc := range c.Scenes {
		if sc.Name == "" {
			errs = append(errs, fmt.Errorf("channel %s: scenes[%d] has no name", c.Name, i))
		}
		if sc.From == sc.To {
			errs = append(errs, fmt.Errorf("channel %s: scene %q: from and to are the same", c.Name, sc.Name))
		}
		if !sc.View.IsZero() {
			if sc.Sprite != "" {
				errs = append(errs, fmt.Errorf("channel %s: scene %q: give a sprite or a view, not both", c.Name, sc.Name))
			}
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
