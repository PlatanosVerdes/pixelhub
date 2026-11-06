package talk

import (
	"fmt"
	"image"
	"image/color"
	"strings"
)

// Shape is how open the mouth is, or a blink with it shut.
type Shape int

const (
	Shut Shape = iota
	Small
	Half
	Wide
	Blink
)

// Mood is what the eyes do, set by the text being said.
type Mood int

const (
	Calm Mood = iota
	Excited
	Asking
	Thinking
	Laughing
)

const (
	eyeRow   = 2
	mouthRow = 10
)

// Mosko is the talking face: an orange square with heavy-lidded eyes and a mouth below.
var mosko = struct {
	palette map[rune]color.RGBA
	eyes    map[Mood][]string
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
	eyes: map[Mood][]string{
		Calm: {
			"OOOOOOOOOOOOOOOO",
			"OOOOOOOOOOOOOOOO",
			"ODDDDDOOOODDDDDO",
			"OKKKKKOOOOKKKKKO",
			"OWWPPWOOOOWPPWWO",
			"ODDDDDOOOODDDDDO",
			"OOOOOOOOOOOOOOOO",
		},
		Excited: {
			"OKKKKOOOOOOKKKKO",
			"OOOOOOOOOOOOOOOO",
			"OKKKKKOOOOKKKKKO",
			"OWWPPWOOOOWPPWWO",
			"OWWPPWOOOOWPPWWO",
			"OWWWWWOOOOWWWWWO",
			"ODDDDDOOOODDDDDO",
		},
		Asking: {
			"OKKKKOOOOOOOOOOO",
			"OOOOOOOOOOOOOOOO",
			"OKKKKKOOOODDDDDO",
			"OWWPPWOOOOKKKKKO",
			"OWWPPWOOOOWPPWWO",
			"ODDDDDOOOODDDDDO",
			"OOOOOOOOOOOOOOOO",
		},
		Thinking: {
			"OOOOOOOOOOOKKKKO",
			"OOOOOOOOOOOOOOOO",
			"ODDDDDOOOODDDDDO",
			"OKKKKKOOOOKKKKKO",
			"OWWWPPOOOOWWWPPO",
			"ODDDDDOOOODDDDDO",
			"OOOOOOOOOOOOOOOO",
		},
		Laughing: {
			"OOOOOOOOOOOOOOOO",
			"OOOOOOOOOOOOOOOO",
			"OOOOOOOOOOOOOOOO",
			"OOKKKOOOOOOKKKOO",
			"OKOOOKOOOOKOOOKO",
			"OOOOOOOOOOOOOOOO",
			"OOOOOOOOOOOOOOOO",
		},
	},
	blink: []string{
		"OOOOOOOOOOOOOOOO",
		"OOOOOOOOOOOOOOOO",
		"ODDDDDOOOODDDDDO",
		"ODDDDDOOOODDDDDO",
		"OKKKKKOOOOKKKKKO",
		"OOOOOOOOOOOOOOOO",
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

// Face draws Mosko with a mouth and a mood, 16x16. A blink shuts the eyes and the mouth.
func Face(s Shape, m Mood) image.Image {
	rows := make([]string, 16)
	for i := range rows {
		rows[i] = strings.Repeat("O", 16)
	}
	eyes, mouth := mosko.eyes[m], mosko.mouths[s]
	if s == Blink {
		eyes, mouth = mosko.blink, mosko.mouths[Shut]
	}
	copy(rows[eyeRow:], eyes)
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
