package display

import (
	"errors"
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// Mode is one of the display's own modes, the ones its side button cycles through.
type Mode string

const (
	ModeClock      Mode = "clock"
	ModeLight      Mode = "light"
	ModeVisualizer Mode = "visualizer"
	ModeAnimations Mode = "animations"
	ModeScoreboard Mode = "scoreboard"
	ModeStopwatch  Mode = "stopwatch"
	ModeCountdown  Mode = "countdown"
	ModeNoise      Mode = "noise"
)

// Tools are the display's built-in apps; Running starts or stops them.
func (m Mode) Tool() bool { return m == ModeStopwatch || m == ModeCountdown || m == ModeNoise }

// Styles is how many styles each mode has: clock faces, light effects, visualisers.
var Styles = map[Mode]int{ModeClock: 6, ModeLight: 5, ModeVisualizer: 12}

const (
	MaxScore     = 999
	MaxCountdown = 99*60 + 59
)

// View is a mode with its options; the zero View is no mode. Animations plays the list kept on the
// display itself, which is never written from here.
type View struct {
	Mode        Mode   `json:"mode"`
	Style       int    `json:"style,omitempty"`
	Weather     bool   `json:"weather,omitempty"`
	Temperature bool   `json:"temperature,omitempty"`
	Calendar    bool   `json:"calendar,omitempty"`
	Color       string `json:"color,omitempty"`
	Red         int    `json:"red,omitempty"`
	Blue        int    `json:"blue,omitempty"`
	// Seconds is the countdown's length.
	Seconds int  `json:"seconds,omitempty"`
	Running bool `json:"running,omitempty"`
}

func (v View) IsZero() bool { return v == View{} }

func (v View) Validate() error {
	var errs []error
	switch v.Mode {
	case ModeClock, ModeLight, ModeVisualizer, ModeAnimations, ModeScoreboard, ModeStopwatch, ModeCountdown, ModeNoise:
	default:
		return fmt.Errorf("mode %q: want clock, light, visualizer, animations, scoreboard, stopwatch, countdown or noise", v.Mode)
	}
	if n := Styles[v.Mode]; v.Style < 0 || v.Style >= max(n, 1) {
		errs = append(errs, fmt.Errorf("%s style %d is outside 0-%d", v.Mode, v.Style, max(n-1, 0)))
	}
	if v.Color != "" {
		if _, err := ParseColor(v.Color); err != nil {
			errs = append(errs, err)
		}
	}
	if (v.Weather || v.Temperature || v.Calendar) && v.Mode != ModeClock {
		errs = append(errs, errors.New("weather, temperature and calendar only go with the clock"))
	}
	if v.Mode != ModeScoreboard && (v.Red != 0 || v.Blue != 0) {
		errs = append(errs, errors.New("red and blue only go with the scoreboard"))
	}
	if v.Red < 0 || v.Red > MaxScore || v.Blue < 0 || v.Blue > MaxScore {
		errs = append(errs, fmt.Errorf("scores must be 0-%d", MaxScore))
	}
	if v.Running && !v.Mode.Tool() {
		errs = append(errs, errors.New("running only goes with stopwatch, countdown and noise"))
	}
	if v.Mode == ModeCountdown && (v.Seconds < 1 || v.Seconds > MaxCountdown) {
		errs = append(errs, fmt.Errorf("countdown seconds must be 1-%d", MaxCountdown))
	}
	if v.Mode != ModeCountdown && v.Seconds != 0 {
		errs = append(errs, errors.New("seconds only go with the countdown"))
	}
	return errors.Join(errs...)
}

// RGB is the view's colour, white when it has none.
func (v View) RGB() color.RGBA {
	c, err := ParseColor(v.Color)
	if err != nil {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return c
}

func (v View) Content() Content {
	return Content{Kind: Native, Name: v.String(), View: v}
}

func (v View) String() string {
	parts := []string{string(v.Mode)}
	switch v.Mode {
	case ModeClock, ModeVisualizer, ModeLight:
		parts = append(parts, strconv.Itoa(v.Style))
	case ModeScoreboard:
		parts = append(parts, fmt.Sprintf("%d-%d", v.Red, v.Blue))
	case ModeCountdown:
		parts = append(parts, fmt.Sprintf("%d:%02d", v.Seconds/60, v.Seconds%60))
	}
	if v.Mode.Tool() && !v.Running {
		parts = append(parts, "stopped")
	}
	if v.Color != "" {
		parts = append(parts, strings.ToLower(v.Color))
	}
	return strings.Join(parts, " ")
}

// ParseColor reads #rrggbb.
func ParseColor(s string) (color.RGBA, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil || len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, fmt.Errorf("color %q: want #rrggbb", s)
	}
	return color.RGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 255}, nil
}
