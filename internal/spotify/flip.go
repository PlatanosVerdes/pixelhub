package spotify

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	// flipFor is how long the music layer plays the flip before the still cover replaces it.
	flipFor   = 2 * time.Second
	flipFrame = 55 * time.Millisecond
	flipHold  = 5 * time.Second
	// tilt is how much shorter the far edge of the turning cover gets, for depth.
	tilt = 0.35
)

// flip turns the old cover over like a card and brings the new one round; without an old cover the
// back of the card, a record, turns first.
func flip(old, cover image.Image) []display.Frame {
	size := cover.Bounds().Dx()
	if old == nil {
		old = record(cover, size)
	}
	var frames []display.Frame
	for _, deg := range []float64{25, 50, 70, 85} {
		frames = append(frames, display.Frame{Image: turned(old, size, deg, false), Delay: flipFrame})
	}
	for _, deg := range []float64{85, 70, 50, 25} {
		frames = append(frames, display.Frame{Image: turned(cover, size, deg, true), Delay: flipFrame})
	}
	return append(frames, display.Frame{Image: cover, Delay: flipHold})
}

// turned draws src rotated deg degrees about its vertical axis: narrower, darker, and with the far
// edge shorter. coming puts the far edge on the left, as the new face swings in.
func turned(src image.Image, size int, deg float64, coming bool) *image.NRGBA {
	rad := deg * math.Pi / 180
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := range out.Pix {
		if i%4 == 3 {
			out.Pix[i] = 255
		}
	}
	w := max(1, int(math.Round(float64(size)*math.Cos(rad))))
	x0 := (size - w) / 2
	shade := 0.55 + 0.45*math.Cos(rad)
	b := src.Bounds()
	for i := range w {
		u := (float64(i) + 0.5) / float64(w)
		far := u
		if coming {
			far = 1 - u
		}
		h := max(1, int(math.Round(float64(size)*(1-tilt*math.Sin(rad)*far))))
		y0 := (size - h) / 2
		sx := b.Min.X + min(size-1, int(u*float64(size)))
		for j := range h {
			sy := b.Min.Y + min(size-1, (j*size)/h)
			r, g, bl, _ := src.At(sx, sy).RGBA()
			out.SetNRGBA(x0+i, y0+j, color.NRGBA{R: uint8(float64(r>>8) * shade), G: uint8(float64(g>>8) * shade), B: uint8(float64(bl>>8) * shade), A: 255})
		}
	}
	return out
}

// record is the back of the card: a dark disc with two grooves and a label in the cover's mean
// colour, brightened so a dark cover still gives a label that shows.
func record(cover image.Image, size int) *image.NRGBA {
	label := brighten(mean(cover))
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	c := float64(size-1) / 2
	edge, outer, inner := float64(size)*0.47, float64(size)*0.38, float64(size)*0.27
	for y := range size {
		for x := range size {
			d := math.Hypot(float64(x)-c, float64(y)-c)
			px := color.NRGBA{A: 255}
			switch {
			case d < 0.8:
			case d < 2.6:
				px = label
			case d < edge:
				px = color.NRGBA{R: 36, G: 36, B: 42, A: 255}
				if math.Abs(d-inner) < 0.45 || math.Abs(d-outer) < 0.45 {
					px = color.NRGBA{R: 100, G: 100, B: 112, A: 255}
				}
			}
			out.SetNRGBA(x, y, px)
		}
	}
	return out
}

func mean(img image.Image) color.NRGBA {
	b := img.Bounds()
	var r, g, bl, n uint64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			r, g, bl, n = r+uint64(cr>>8), g+uint64(cg>>8), bl+uint64(cb>>8), n+1
		}
	}
	return color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: 255}
}

func brighten(c color.NRGBA) color.NRGBA {
	top := max(c.R, c.G, c.B, 1)
	k := 230 / float64(top)
	return color.NRGBA{R: uint8(float64(c.R) * k), G: uint8(float64(c.G) * k), B: uint8(float64(c.B) * k), A: 255}
}
