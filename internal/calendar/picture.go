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

// Picture is a calendar page with the minutes left, its header blinking.
func Picture(left time.Duration, size int) display.Content {
	minutes := min(99, max(1, int((left+time.Minute-1)/time.Minute)))
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
		text := []int{minutes}
		if minutes >= 10 {
			text = []int{minutes / 10, minutes % 10}
		}
		x0 := (size - (len(text)*6 - 1)) / 2
		for i, d := range text {
			for row, line := range digits[d] {
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
