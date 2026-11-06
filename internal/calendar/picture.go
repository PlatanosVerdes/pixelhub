package calendar

import (
	"image"
	"image/color"
	"strconv"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

var (
	paper  = color.RGBA{170, 170, 180, 255}
	header = color.RGBA{230, 0, 40, 255}
	dim    = color.RGBA{70, 0, 12, 255}
	ring   = color.RGBA{255, 255, 255, 255}
	digit  = color.RGBA{255, 255, 255, 255}
)

// 5x7 digits, one string per row.
var digits = [10][7]string{
	{".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	{"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", ".###."},
	{".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	{"####.", "....#", "....#", ".###.", "....#", "....#", "####."},
	{"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	{"#####", "#....", "####.", "....#", "....#", "#...#", ".###."},
	{".###.", "#....", "#....", "####.", "#...#", "#...#", ".###."},
	{"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	{".###.", "#...#", "#...#", ".###.", "#...#", "#...#", ".###."},
	{".###.", "#...#", "#...#", ".####", "....#", "....#", ".###."},
}

var (
	bang  = [7]string{"..#..", "..#..", "..#..", "..#..", "..#..", ".....", "..#.."}
	hours = [7]string{"#....", "#....", "####.", "#...#", "#...#", "#...#", "#...#"}
	days  = [7]string{"....#", "....#", ".####", "#...#", "#...#", "#...#", ".####"}
)

// Picture is a calendar page with the time left (37, or 3h and 2d past 99 minutes), or a ! once the
// event starts, its header blinking.
func Picture(left time.Duration, size int) display.Content {
	minutes := 0
	if left > 0 {
		minutes = max(1, int((left+time.Minute-1)/time.Minute))
	}
	frame := func(top color.RGBA) display.Frame {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		for y := 1; y < size; y++ {
			for x := range size {
				switch {
				case y <= 4:
					img.Set(x, y, top)
				case x == 0 || x == size-1 || y == size-1:
					img.Set(x, y, paper)
				default:
					img.Set(x, y, color.RGBA{A: 255})
				}
			}
		}
		for _, x := range []int{4, size - 5} {
			for y := range 3 {
				img.Set(x, y, ring)
			}
		}
		glyphs := glyphsFor(minutes)
		x0 := (size - (len(glyphs)*6 - 1)) / 2
		for i, g := range glyphs {
			for row, line := range g {
				for col, c := range line {
					if c == '#' {
						img.Set(x0+i*6+col, 6+row, digit)
					}
				}
			}
		}
		return display.Frame{Image: img, Delay: 600 * time.Millisecond}
	}
	return display.Content{Kind: display.Picture, Name: "calendar", Key: strconv.Itoa(minutes), Frames: []display.Frame{frame(header), frame(dim)}}
}

// glyphsFor is the time left as it fits on the page: 37, 3h below 10 hours, then whole days rounded
// up, so an event tomorrow morning reads 1d.
func glyphsFor(minutes int) [][7]string {
	const day = 24 * 60
	switch {
	case minutes <= 0:
		return [][7]string{bang}
	case minutes < 10:
		return [][7]string{digits[minutes]}
	case minutes < 100:
		return [][7]string{digits[minutes/10], digits[minutes%10]}
	case minutes < 10*60:
		return [][7]string{digits[minutes/60], hours}
	case minutes <= 9*day:
		return [][7]string{digits[(minutes+day-1)/day], days}
	}
	return [][7]string{bang}
}
