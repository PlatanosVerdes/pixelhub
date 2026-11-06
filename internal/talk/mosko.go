package talk

import (
	"fmt"
	"image"
	"image/color"
)

// Shape is one look of the face: how open the mouth is, or a blink with it shut.
type Shape int

const (
	Shut Shape = iota
	Small
	Half
	Wide
	Blink
)

// Mosko is the talking face: an orange square with heavy-lidded eyes and a mouth below.
var mosko = struct {
	palette map[rune]color.RGBA
	face    []string
	blink   []string
	mouths  map[Shape][]string
}{
	palette: map[rune]color.RGBA{
		'O': {255, 166, 0, 255},
		'D': {222, 128, 0, 255},
		'K': {70, 32, 6, 255},
		'W': {255, 244, 214, 255},
		'P': {28, 12, 2, 255},
		'L': {120, 48, 0, 255},
		'M': {96, 16, 18, 255},
		'T': {236, 92, 92, 255},
	},
	face: []string{
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"ODDDDDOOOODDDDDO",
		"OKKKKKOOOOKKKKKO",
		"OWWPPWOOOOWPPWWO",
		"ODDDDDOOOODDDDDO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
	},
	blink: []string{
		"ODDDDDOOOODDDDDO",
		"ODDDDDOOOODDDDDO",
		"OKKKKKOOOOKKKKKO",
		"OOOOOOOOOOOOOOOO",
	},
	mouths: map[Shape][]string{
		Shut: {
			"OOOOOOOOOOOOOOOO",
			"OOOOOLLLLLLOOOOO",
			"OOOOOOOOOOOOOOOO",
			"OOOOOOOOOOOOOOOO",
			"OOOOOOOOOOOOOOOO",
		},
		Small: {
			"OOOOOOOOOOOOOOOO",
			"OOOOOLLLLLLOOOOO",
			"OOOOOLMMMMLOOOOO",
			"OOOOOOLLLLOOOOOO",
			"OOOOOOOOOOOOOOOO",
		},
		Half: {
			"OOOOOLLLLLLOOOOO",
			"OOOOLMMMMMMLOOOO",
			"OOOOLMMTTMMLOOOO",
			"OOOOOLLLLLLOOOOO",
			"OOOOOOOOOOOOOOOO",
		},
		Wide: {
			"OOOOLLLLLLLLOOOO",
			"OOOLMMMMMMMMLOOO",
			"OOOLMMMMMMMMLOOO",
			"OOOLMMTTTTMMLOOO",
			"OOOOLLLLLLLLOOOO",
		},
	},
}

const (
	eyeRow   = 4
	mouthRow = 10
)

// Face draws Mosko in one shape, 16x16.
func Face(s Shape) image.Image {
	rows := append([]string(nil), mosko.face...)
	mouth := mosko.mouths[s]
	if s == Blink {
		copy(rows[eyeRow:], mosko.blink)
		mouth = mosko.mouths[Shut]
	}
	copy(rows[mouthRow:], mouth)
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y, row := range rows {
		for x, ch := range row {
			c, ok := mosko.palette[ch]
			if !ok {
				panic(fmt.Sprintf("mosko: %q at %d,%d is not in the palette", ch, x, y))
			}
			img.Set(x, y, c)
		}
	}
	return img
}
