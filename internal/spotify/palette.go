package spotify

import (
	"image"
	"image/color"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

// animColors is how many colours an animation's frames share. The display sends each frame with its
// own palette and as many bits per pixel as that palette needs, so fewer colours make an animation
// a fraction of the size and it reaches the display in well under a second.
const animColors = 32

// reduce maps every frame to one palette: black and the colours that best cover the frames.
func reduce(frames []display.Frame) []display.Frame {
	imgs := make([]image.Image, len(frames))
	for i, f := range frames {
		imgs[i] = f.Image
	}
	pal := palette(animColors-1, imgs...)
	pal = append(pal, color.NRGBA{A: 255})
	out := make([]display.Frame, len(frames))
	for i, f := range frames {
		b := f.Image.Bounds()
		img := image.NewNRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				img.SetNRGBA(x, y, nearest(pal, f.Image.At(x, y)))
			}
		}
		out[i] = display.Frame{Image: img, Delay: f.Delay}
	}
	return out
}

// palette picks k colours by k-means over the pixels of the images, seeded with the colours furthest
// apart so a small bright detail keeps its own colour.
func palette(k int, imgs ...image.Image) []color.NRGBA {
	var px [][3]float64
	for _, img := range imgs {
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				px = append(px, [3]float64{float64(r >> 8), float64(g >> 8), float64(bl >> 8)})
			}
		}
	}
	if len(px) == 0 {
		return nil
	}
	centres := [][3]float64{px[0]}
	for len(centres) < k {
		best, far := px[0], -1.0
		for _, p := range px {
			d := distanceTo(centres, p)
			if d > far {
				best, far = p, d
			}
		}
		if far == 0 {
			break
		}
		centres = append(centres, best)
	}
	for range 8 {
		sums := make([][4]float64, len(centres))
		for _, p := range px {
			i := closest(centres, p)
			sums[i] = [4]float64{sums[i][0] + p[0], sums[i][1] + p[1], sums[i][2] + p[2], sums[i][3] + 1}
		}
		for i, s := range sums {
			if s[3] > 0 {
				centres[i] = [3]float64{s[0] / s[3], s[1] / s[3], s[2] / s[3]}
			}
		}
	}
	out := make([]color.NRGBA, len(centres))
	for i, c := range centres {
		out[i] = color.NRGBA{R: uint8(c[0] + 0.5), G: uint8(c[1] + 0.5), B: uint8(c[2] + 0.5), A: 255}
	}
	return out
}

func distanceTo(centres [][3]float64, p [3]float64) float64 {
	return dist(centres[closest(centres, p)], p)
}

func closest(centres [][3]float64, p [3]float64) int {
	best, bd := 0, -1.0
	for i, c := range centres {
		if d := dist(c, p); bd < 0 || d < bd {
			best, bd = i, d
		}
	}
	return best
}

// dist weighs green most and blue least, as the eye does.
func dist(a, b [3]float64) float64 {
	dr, dg, db := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return 2*dr*dr + 4*dg*dg + 3*db*db
}

func nearest(pal []color.NRGBA, c color.Color) color.NRGBA {
	r, g, b, _ := c.RGBA()
	p := [3]float64{float64(r >> 8), float64(g >> 8), float64(b >> 8)}
	best, bd := pal[0], -1.0
	for _, q := range pal {
		if d := dist([3]float64{float64(q.R), float64(q.G), float64(q.B)}, p); bd < 0 || d < bd {
			best, bd = q, d
		}
	}
	return best
}
