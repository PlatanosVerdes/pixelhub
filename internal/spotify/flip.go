package spotify

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	// flipFor is how long the music layer plays the half turn to a new cover before the idle one.
	flipFor   = 2 * time.Second
	flipFrame = 40 * time.Millisecond
	flipHold  = 5 * time.Second
	// idleTurn is how long the cover stays still before it turns round twice on its own, idlePause
	// the short stop between those two full turns.
	idleTurn  = 20 * time.Second
	idlePause = 500 * time.Millisecond
	// distance is how far the eye is from the cover, in cover widths: closer gives more perspective.
	distance = 1.6
	// samples per pixel side, averaged so the turning edges come out smooth.
	samples = 4
)

// flip turns the old cover half a turn about its vertical axis and brings the new one round.
func flip(old, cover image.Image) []display.Frame {
	return append(halfTurn(old, cover), display.Frame{Image: cover, Delay: flipHold})
}

// idle holds the cover still, then turns it a full turn twice with a short stop in between; the
// display loops it.
func idle(cover image.Image) []display.Frame {
	turn := append(halfTurn(cover, cover), halfTurn(cover, cover)...)
	frames := append([]display.Frame{{Image: cover, Delay: idleTurn}}, turn...)
	frames = append(frames, display.Frame{Image: cover, Delay: idlePause})
	return append(frames, turn...)
}

func halfTurn(old, cover image.Image) []display.Frame {
	size := cover.Bounds().Dx()
	var frames []display.Frame
	for _, deg := range []float64{22, 45, 66, 84} {
		frames = append(frames, display.Frame{Image: turned(old, size, deg), Delay: flipFrame})
	}
	for _, deg := range []float64{84, 66, 45, 22} {
		frames = append(frames, display.Frame{Image: turned(cover, size, -deg), Delay: flipFrame})
	}
	return frames
}

// turned draws src rotated deg degrees about its vertical axis, seen in perspective: the edge going
// away gets shorter and the cover darkens as it turns side on. Outside the cover is black.
func turned(src image.Image, size int, deg float64) *image.NRGBA {
	rad := deg * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	shade := 0.7 + 0.3*cos
	b := src.Bounds()
	half := float64(size) / 2
	d := distance * 2
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := range size {
		for px := range size {
			var r, g, bl float64
			for sy := range samples {
				for sx := range samples {
					// The output point, centred and scaled so the cover spans -1..1.
					X := (float64(px)+(float64(sx)+0.5)/samples)/half - 1
					Y := (float64(py)+(float64(sy)+0.5)/samples)/half - 1
					// Solve for the point on the cover that projects to X, then scale Y by its depth.
					den := cos*d - X*sin
					if den <= 0 {
						continue
					}
					x := X * d / den
					y := Y * (d + x*sin) / d
					if x < -1 || x >= 1 || y < -1 || y >= 1 {
						continue
					}
					cr, cg, cb, _ := src.At(b.Min.X+int((x+1)*half), b.Min.Y+int((y+1)*half)).RGBA()
					r, g, bl = r+float64(cr>>8), g+float64(cg>>8), bl+float64(cb>>8)
				}
			}
			n := float64(samples*samples) / shade
			out.SetNRGBA(px, py, color.NRGBA{R: uint8(min(255, r/n)), G: uint8(min(255, g/n)), B: uint8(min(255, bl/n)), A: 255})
		}
	}
	return out
}
