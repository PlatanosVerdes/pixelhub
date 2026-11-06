package calendar

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

var (
	paper  = color.RGBA{170, 170, 180, 255}
	header = color.RGBA{230, 0, 40, 255}
	dim    = color.RGBA{70, 0, 12, 255}
	ring   = color.RGBA{255, 255, 255, 255}
	digit  = color.RGBA{255, 255, 255, 255}
	text   = color.RGBA{255, 210, 0, 255}
	black  = color.RGBA{A: 255}
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

// 3x5 letters for the event's title, three columns per row.
var small = map[rune][5]string{
	'A': {".#.", "#.#", "###", "#.#", "#.#"}, 'B': {"##.", "#.#", "##.", "#.#", "##."},
	'C': {".##", "#..", "#..", "#..", ".##"}, 'D': {"##.", "#.#", "#.#", "#.#", "##."},
	'E': {"###", "#..", "##.", "#..", "###"}, 'F': {"###", "#..", "##.", "#..", "#.."},
	'G': {".##", "#..", "#.#", "#.#", ".##"}, 'H': {"#.#", "#.#", "###", "#.#", "#.#"},
	'I': {"###", ".#.", ".#.", ".#.", "###"}, 'J': {"..#", "..#", "..#", "#.#", ".#."},
	'K': {"#.#", "#.#", "##.", "#.#", "#.#"}, 'L': {"#..", "#..", "#..", "#..", "###"},
	'M': {"#.#", "###", "###", "#.#", "#.#"}, 'N': {"##.", "#.#", "#.#", "#.#", "#.#"},
	'O': {".#.", "#.#", "#.#", "#.#", ".#."}, 'P': {"##.", "#.#", "##.", "#..", "#.."},
	'Q': {".#.", "#.#", "#.#", "##.", ".##"}, 'R': {"##.", "#.#", "##.", "#.#", "#.#"},
	'S': {".##", "#..", ".#.", "..#", "##."}, 'T': {"###", ".#.", ".#.", ".#.", ".#."},
	'U': {"#.#", "#.#", "#.#", "#.#", "###"}, 'V': {"#.#", "#.#", "#.#", "#.#", ".#."},
	'W': {"#.#", "#.#", "###", "###", "#.#"}, 'X': {"#.#", "#.#", ".#.", "#.#", "#.#"},
	'Y': {"#.#", "#.#", ".#.", ".#.", ".#."}, 'Z': {"###", "..#", ".#.", "#..", "###"},
	'0': {"###", "#.#", "#.#", "#.#", "###"}, '1': {".#.", "##.", ".#.", ".#.", "###"},
	'2': {"##.", "..#", ".#.", "#..", "###"}, '3': {"##.", "..#", ".#.", "..#", "##."},
	'4': {"#.#", "#.#", "###", "..#", "..#"}, '5': {"###", "#..", "##.", "..#", "##."},
	'6': {".##", "#..", "###", "#.#", "###"}, '7': {"###", "..#", ".#.", ".#.", ".#."},
	'8': {"###", "#.#", "###", "#.#", "###"}, '9': {"###", "#.#", "###", "..#", "##."},
	' ': {"...", "...", "...", "...", "..."}, '.': {"...", "...", "...", "...", ".#."},
	',': {"...", "...", "...", ".#.", "#.."}, ':': {"...", ".#.", "...", ".#.", "..."},
	'-': {"...", "...", "###", "...", "..."}, '!': {".#.", ".#.", ".#.", "...", ".#."},
	'?': {"##.", "..#", ".#.", "...", ".#."}, '/': {"..#", "..#", ".#.", "#..", "#.."},
	'&': {".#.", "#.#", ".#.", "#.#", ".##"}, '\'': {".#.", ".#.", "...", "...", "..."},
	'(': {".#.", "#..", "#..", "#..", ".#."}, ')': {".#.", "..#", "..#", "..#", ".#."},
	'+': {"...", ".#.", "###", ".#.", "..."}, '@': {"###", "#.#", "#.#", "#..", "###"},
}

var unaccent = strings.NewReplacer("Á", "A", "À", "A", "Ä", "A", "Â", "A", "É", "E", "È", "E", "Ë", "E", "Ê", "E",
	"Í", "I", "Ì", "I", "Ï", "I", "Î", "I", "Ó", "O", "Ò", "O", "Ö", "O", "Ô", "O", "Ú", "U", "Ù", "U", "Ü", "U", "Û", "U",
	"Ñ", "N", "Ç", "C", "¿", "", "¡", "")

const (
	maxFrames  = 60
	slideIn    = 4
	blinks     = 6
	slideDelay = 70 * time.Millisecond
	blinkDelay = 300 * time.Millisecond
	scrollStep = 120 * time.Millisecond
	maxStep    = 2
	textTop    = 7
)

// Picture is a calendar page that slides in, blinks the time left (37, 3h, 2d, or ! once the event
// starts) and then scrolls the event's title across the page.
func Picture(left time.Duration, title string, size int) display.Content {
	minutes := 0
	if left > 0 {
		minutes = max(1, int((left+time.Minute-1)/time.Minute))
	}
	label := glyphsFor(minutes)
	var frames []display.Frame
	for i := range slideIn {
		frames = append(frames, display.Frame{Image: page(size, header, label, nil, 0, -(slideIn-i)*3), Delay: slideDelay})
	}
	for i := range blinks {
		top := header
		if i%2 == 1 {
			top = dim
		}
		frames = append(frames, display.Frame{Image: page(size, top, label, nil, 0, 0), Delay: blinkDelay})
	}
	if cols := titleColumns(title); len(cols) > 0 {
		// The title enters at the right edge and leaves at the left, one or two columns a frame to fit
		// what is left of the animation; past that it is cut, since faster cannot be read.
		room := maxFrames - len(frames)
		cols = cols[:min(len(cols), (room-1)*maxStep-(size-2))]
		travel := len(cols) + size - 2
		step := min(maxStep, (travel+room-2)/(room-1))
		for shift := 0; shift <= travel; shift += step {
			frames = append(frames, display.Frame{Image: page(size, header, nil, cols, size-1-shift, 0), Delay: scrollStep * time.Duration(step)})
		}
	}
	return display.Content{Kind: display.Picture, Name: "calendar", Key: strconv.Itoa(minutes) + "/" + title, Frames: frames}
}

// page draws the calendar page moved down by dy (negative while it slides in), with either the big
// label or the title's columns starting at x.
func page(size int, top color.RGBA, label [][7]string, cols [][5]bool, x, dy int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for px := range size {
			img.Set(px, y, black)
		}
	}
	set := func(px, py int, c color.RGBA) {
		if py += dy; py >= 0 && py < size && px >= 0 && px < size {
			img.Set(px, py, c)
		}
	}
	for y := 1; y < size; y++ {
		for px := range size {
			switch {
			case y <= 4:
				set(px, y, top)
			case px == 0 || px == size-1 || y == size-1:
				set(px, y, paper)
			}
		}
	}
	for _, rx := range []int{4, size - 5} {
		for y := range 3 {
			set(rx, y, ring)
		}
	}
	x0 := (size - (len(label)*6 - 1)) / 2
	for i, g := range label {
		for row, line := range g {
			for col, c := range line {
				if c == '#' {
					set(x0+i*6+col, 6+row, digit)
				}
			}
		}
	}
	for i, col := range cols {
		px := x + i
		if px < 1 || px > size-2 {
			continue
		}
		for row, on := range col {
			if on {
				set(px, textTop+row, text)
			}
		}
	}
	return img
}

// titleColumns lays the title out in the small font, one column of five pixels per entry, with a
// blank column between letters; accents are dropped and letters the font lacks are skipped.
func titleColumns(title string) [][5]bool {
	var cols [][5]bool
	plain := unaccent.Replace(strings.ToUpper(strings.Join(strings.Fields(title), " ")))
	for _, r := range plain {
		g, ok := small[r]
		if !ok {
			continue
		}
		for c := range 3 {
			var col [5]bool
			for row := range 5 {
				col[row] = g[row][c] == '#'
			}
			cols = append(cols, col)
		}
		cols = append(cols, [5]bool{})
	}
	return cols
}

// Quiet is the night, 23:00 to 08:00, when an event starting does not chime.
func Quiet(t time.Time) bool {
	return t.Hour() >= 23 || t.Hour() < 8
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
