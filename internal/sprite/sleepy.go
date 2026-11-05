package sprite

import (
	"image"
	"image/color"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

// digits is a 3x5 font, one row of three bits per line, top first.
var digits = [10][5]uint8{
	{7, 5, 5, 5, 7}, {2, 6, 2, 2, 7}, {7, 1, 7, 4, 7}, {7, 1, 3, 1, 7}, {5, 5, 7, 1, 1},
	{7, 4, 7, 1, 7}, {7, 4, 7, 5, 7}, {7, 1, 1, 2, 2}, {7, 5, 7, 5, 7}, {7, 5, 7, 1, 7},
}

var zs = []image.Point{
	// a 4x4 Z, then a 3x3 one lower down
	{11, 2}, {12, 2}, {13, 2}, {14, 2}, {13, 3}, {12, 4}, {11, 5}, {12, 5}, {13, 5}, {14, 5},
	{12, 8}, {13, 8}, {14, 8}, {13, 9}, {12, 10}, {13, 10}, {14, 10},
}

// SleepyClock is the time on two lines with a "zz", for waking the display briefly at night.
func SleepyClock(t time.Time, c color.RGBA) display.Content {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	draw := func(d, x, y int) {
		for row, bits := range digits[d] {
			for col := range 3 {
				if bits&(4>>col) != 0 {
					img.SetRGBA(x+col, y+row, c)
				}
			}
		}
	}
	h, m := t.Hour(), t.Minute()
	draw(h/10, 1, 2)
	draw(h%10, 5, 2)
	draw(m/10, 1, 9)
	draw(m%10, 5, 9)
	dim := color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255}
	for _, p := range zs {
		img.SetRGBA(p.X, p.Y, dim)
	}
	return display.Content{Kind: display.Picture, Name: "sleepy clock", Key: t.Format("15:04"), Frames: []display.Frame{{Image: img}}}
}
