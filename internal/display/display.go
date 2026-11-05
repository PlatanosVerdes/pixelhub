package display

import (
	"fmt"
	"image"
	"image/color"
	"time"
)

type Kind int

const (
	Clock Kind = iota
	Color
	Picture
	// Native is one of the display's own modes, described by View.
	Native
)

type Frame struct {
	Image image.Image
	Delay time.Duration
}

// Content is what a display should show. Picture uses Frames (one is a still image), Color uses Fill,
// Native uses View.
type Content struct {
	Kind Kind
	Name string
	// Key tells apart pictures that share a name.
	Key        string
	Frames     []Frame
	Fill       color.RGBA
	View       View
	Brightness int
	// Weather feeds the clock's weather and temperature.
	Weather Weather
}

// Weather is the outside reading; Sky uses the display's own codes.
type Weather struct {
	Known   bool `json:"known"`
	Celsius int  `json:"celsius"`
	Sky     Sky  `json:"sky"`
}

type Sky int

const (
	SkyClear  Sky = 1
	SkyCloudy Sky = 3
	SkyStorm  Sky = 5
	SkyRain   Sky = 6
	SkySnow   Sky = 8
	SkyFog    Sky = 9
)

func (c Content) WithBrightness(level int) Content {
	c.Brightness = level
	return c
}

// SamePicture reports whether two contents show the same thing, perhaps at different brightness.
func (c Content) SamePicture(o Content) bool {
	return c.Kind == o.Kind && c.Name == o.Name && c.Key == o.Key && c.Fill == o.Fill && c.View == o.View && c.Weather == o.Weather
}

func ClockContent() Content { return Content{Kind: Clock, Name: "clock"} }

func ColorContent(fill color.RGBA) Content {
	return Content{Kind: Color, Name: fmt.Sprintf("color #%02x%02x%02x", fill.R, fill.G, fill.B), Fill: fill}
}

type Status struct {
	Address   string    `json:"address"`
	Connected bool      `json:"connected"`
	Acks      int       `json:"acks"`
	LastAck   time.Time `json:"last_ack,omitzero"`
	// Device is the mode the display last reported, with the colour and style it keeps for it.
	Device *View `json:"device,omitempty"`
	// Battery is the last level the display reported, 0 (empty) to 9 (full), when the bridge can read it.
	Battery   *int      `json:"battery,omitempty"`
	BatteryAt time.Time `json:"battery_at,omitzero"`
}

// Display drives a physical screen. Show only records what should be on screen: the driver
// paints it when it can and again after every reconnect.
type Display interface {
	Show(Content)
	Release()
	Claim()
	Status() Status
}
