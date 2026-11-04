package sprite

import (
	"image/color"
	"time"
)

type art struct {
	palette map[rune]color.RGBA
	delay   time.Duration
	frames  [][]string
}

var (
	red    = color.RGBA{230, 0, 40, 255}
	white  = color.RGBA{255, 255, 255, 255}
	yellow = color.RGBA{255, 210, 0, 255}
	orange = color.RGBA{255, 120, 0, 255}
	pale   = color.RGBA{240, 225, 150, 255}
	green  = color.RGBA{0, 200, 70, 255}
	blue   = color.RGBA{30, 90, 200, 255}
	sky    = color.RGBA{120, 140, 255, 255}
	silver = color.RGBA{170, 170, 180, 255}
	grey   = color.RGBA{90, 90, 100, 255}
	brown  = color.RGBA{120, 60, 20, 255}
)

var bigHeart = []string{
	"................",
	"................",
	"..RRR......RRR..",
	".RRRRR....RRRRR.",
	"RRWWRRR..RRRRRRR",
	"RRWRRRRRRRRRRRRR",
	"RRRRRRRRRRRRRRRR",
	"RRRRRRRRRRRRRRRR",
	".RRRRRRRRRRRRRR.",
	"..RRRRRRRRRRRR..",
	"...RRRRRRRRRR...",
	"....RRRRRRRR....",
	".....RRRRRR.....",
	"......RRRR......",
	".......RR.......",
	"................",
}

var smallHeart = []string{
	"................",
	"................",
	"................",
	"................",
	"....RR....RR....",
	"...RRRR..RRRR...",
	"...RWRRRRRRRR...",
	"...RRRRRRRRRR...",
	"....RRRRRRRR....",
	".....RRRRRR.....",
	"......RRRR......",
	".......RR.......",
	"................",
	"................",
	"................",
	"................",
}

var builtin = map[string]art{
	"heart": {
		palette: map[rune]color.RGBA{'R': red, 'W': white},
		frames:  [][]string{bigHeart},
	},
	"heartbeat": {
		palette: map[rune]color.RGBA{'R': red, 'W': white},
		delay:   400 * time.Millisecond,
		frames:  [][]string{bigHeart, smallHeart},
	},
	"moon": {
		palette: map[rune]color.RGBA{'Y': pale, 'S': white},
		frames: [][]string{{
			"................",
			".........YYYY...",
			".......YYYYY....",
			"......YYYY......",
			".....YYYY.......",
			".....YYY.....S..",
			"....YYYY........",
			"....YYYY........",
			"....YYYY........",
			"....YYYY..S.....",
			".....YYY........",
			".....YYYY.......",
			"......YYYY......",
			".......YYYYY....",
			".........YYYY...",
			"................",
		}},
	},
	"sun": {
		palette: map[rune]color.RGBA{'Y': yellow, 'O': orange},
		frames: [][]string{{
			"................",
			".......O........",
			".......O........",
			"...O........O...",
			"....O.YYYY.O....",
			".....YYYYYY.....",
			"....YYYYYYYY....",
			".OO.YYYYYYYY.OO.",
			"....YYYYYYYY....",
			"....YYYYYYYY....",
			".....YYYYYY.....",
			"....O.YYYY.O....",
			"...O........O...",
			".......O........",
			".......O........",
			"................",
		}},
	},
	"sunrise": {
		palette: map[rune]color.RGBA{'Y': yellow, 'O': orange, 'H': orange, 'B': blue},
		frames: [][]string{{
			"................",
			"................",
			"................",
			"................",
			"................",
			".......O........",
			".....O....O.....",
			"...O...YY...O...",
			".....YYYYYY.....",
			"..O.YYYYYYYY.O..",
			"....YYYYYYYY....",
			"...YYYYYYYYYY...",
			"HHHHHHHHHHHHHHHH",
			"................",
			".BB..BB..BB..BB.",
			"................",
		}},
	},
	"lunch": {
		palette: map[rune]color.RGBA{'S': silver, 'W': white, 'G': grey},
		frames: [][]string{{
			"................",
			"................",
			".S.S...WWWWWW...",
			".S.S..WWWWWWWW..",
			".S.S.WWWGGGGWWW.",
			".S.SWWWGGGGGGWWW",
			".SSSWWGGGGGGGGWW",
			"..S.WWGGGGGGGGWW",
			"..S.WWGGGGGGGGWW",
			"..S.WWGGGGGGGGWW",
			"..S.WWWGGGGGGWWW",
			"..S..WWWGGGGWWW.",
			"..S...WWWWWWWW..",
			"..S....WWWWWW...",
			"................",
			"................",
		}},
	},
	"coffee": {
		palette: map[rune]color.RGBA{'S': silver, 'C': white, 'B': brown, 'P': grey},
		frames: [][]string{{
			"................",
			"......S...S.....",
			".....S...S......",
			"......S...S.....",
			"................",
			"...CCCCCCCCC....",
			"...CBBBBBBBC....",
			"...CCCCCCCCCCC..",
			"...CCCCCCCCC..C.",
			"...CCCCCCCCC..C.",
			"...CCCCCCCCCCC..",
			"....CCCCCCC.....",
			".....CCCCC......",
			"..PPPPPPPPPPP...",
			"................",
			"................",
		}},
	},
	"zzz": {
		palette: map[rune]color.RGBA{'Z': sky},
		delay:   600 * time.Millisecond,
		frames: [][]string{{
			"................",
			"................",
			"................",
			"................",
			"................",
			"................",
			"................",
			"................",
			"................",
			"................",
			"................",
			".ZZZZ...........",
			"...Z............",
			"..Z.............",
			".ZZZZ...........",
			"................",
		}, {
			"................",
			"................",
			"................",
			"................",
			"................",
			"................",
			".....ZZZZZ......",
			"........Z.......",
			".......Z........",
			"......Z.........",
			".....ZZZZZ......",
			".ZZZZ...........",
			"...Z............",
			"..Z.............",
			".ZZZZ...........",
			"................",
		}, {
			"..........ZZZZZZ",
			"..............Z.",
			".............Z..",
			"............Z...",
			"...........Z....",
			"..........ZZZZZZ",
			".....ZZZZZ......",
			"........Z.......",
			".......Z........",
			"......Z.........",
			".....ZZZZZ......",
			".ZZZZ...........",
			"...Z............",
			"..Z.............",
			".ZZZZ...........",
			"................",
		}},
	},
	"alert": {
		palette: map[rune]color.RGBA{'Y': yellow},
		frames: [][]string{{
			".......YY.......",
			".......YY.......",
			"......YYYY......",
			"......YYYY......",
			".....YYYYYY.....",
			".....YY..YY.....",
			"....YYY..YYY....",
			"....YYY..YYY....",
			"...YYYY..YYYY...",
			"...YYYY..YYYY...",
			"..YYYYY..YYYYY..",
			"..YYYYYYYYYYYY..",
			".YYYYYY..YYYYYY.",
			".YYYYYY..YYYYYY.",
			"YYYYYYYYYYYYYYYY",
			"................",
		}},
	},
	"check": {
		palette: map[rune]color.RGBA{'G': green},
		frames: [][]string{{
			"................",
			"................",
			"................",
			".............GG.",
			"............GGG.",
			"...........GGG..",
			"..........GGG...",
			".GG......GGG....",
			".GGG....GGG.....",
			"..GGG..GGG......",
			"...GGGGGG.......",
			"....GGGG........",
			".....GG.........",
			"................",
			"................",
			"................",
		}},
	},
}

// clockFace stands in for the display's own clock, which pixelhub cannot render.
var clockFace = art{
	palette: map[rune]color.RGBA{'W': silver, 'H': white},
	frames: [][]string{{
		"................",
		"................",
		".....WWWWWW.....",
		"....W......W....",
		"...W...H....W...",
		"..W....H.....W..",
		"..W....H.....W..",
		"..W....H.....W..",
		"..W....HHHH..W..",
		"..W..........W..",
		"..W..........W..",
		"...W........W...",
		"....W......W....",
		".....WWWWWW.....",
		"................",
		"................",
	}},
}
