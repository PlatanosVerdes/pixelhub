package sprite

import (
	"image"
	"image/color"
	"image/draw"
	"sync"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

// Stand-ins for the display's own modes in previews: the display draws them itself.
var modeIcons = map[display.Mode]art{
	display.ModeVisualizer: {palette: map[rune]color.RGBA{'G': green, 'Y': yellow, 'R': red}, frames: [][]string{{
		"................",
		"................",
		"............R...",
		"....R.......R...",
		"....R.......Y...",
		"....Y...R...Y...",
		"....Y...Y...Y.R.",
		"..R.Y...Y...Y.Y.",
		"..Y.G.R.G...G.Y.",
		"..Y.G.Y.G.R.G.G.",
		"..G.G.G.G.Y.G.G.",
		"..G.G.G.G.G.G.G.",
		"..G.G.G.G.G.G.G.",
		"..G.G.G.G.G.G.G.",
		"................",
		"................",
	}}},
	display.ModeAnimations: {palette: map[rune]color.RGBA{'S': sky, 'W': white, 'Y': yellow}, frames: [][]string{{
		"................",
		"................",
		"....SSSSSSSSSS..",
		"....S........S..",
		"..SSSSSSSSSS.S..",
		"..S........S.S..",
		"..S..W.....S.S..",
		"..S..WW....S.S..",
		"..S..WWW...S.S..",
		"..S..WWWW..S.S..",
		"..S..WWW...S.S..",
		"..S..WW....SSS..",
		"..S..W.....S....",
		"..SSSSSSSSSS....",
		"................",
		"................",
	}}},
	display.ModeScoreboard: {palette: map[rune]color.RGBA{'R': red, 'B': blue, 'W': white}, frames: [][]string{{
		"................",
		"................",
		"................",
		"RRRRRRR..BBBBBBB",
		"RRWWWRR..BBWWWBB",
		"RRRRWRR..BBBBWBB",
		"RRWWWRR..BBWWWBB",
		"RRWRRRR..BBBBWBB",
		"RRWWWRR..BBWWWBB",
		"RRRRRRR..BBBBBBB",
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
	}}},
}

var modeImages = sync.OnceValue(func() map[display.Mode]image.Image {
	out := map[display.Mode]image.Image{}
	for mode, a := range modeIcons {
		frames, err := a.render()
		if err != nil {
			panic(err)
		}
		out[mode] = frames[0].Image
	}
	return out
})

func renderView(v display.View, size int) image.Image {
	switch v.Mode {
	case display.ModeLight:
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.Draw(img, img.Bounds(), image.NewUniform(v.RGB()), image.Point{}, draw.Src)
		return img
	case display.ModeClock:
		return Resize(ClockFace(), size)
	}
	if img, ok := modeImages()[v.Mode]; ok {
		return Resize(img, size)
	}
	return Resize(ClockFace(), size)
}
