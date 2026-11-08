package server

import (
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

// theme is something that fits the moment: Label says why, in Spanish like the page, and Words are
// what the gallery is searched for (its names are mostly English).
type theme struct {
	Label  string
	Words  []string
	weight int
}

type day struct{ month, from, to int }

// festivities are dated ones; the gallery has plenty of each.
var festivities = []struct {
	when []day
	theme
}{
	{[]day{{1, 1, 1}}, theme{"Año nuevo", []string{"new year", "fireworks", "party"}, 6}},
	{[]day{{1, 5, 6}}, theme{"Reyes", []string{"king", "crown", "gift"}, 6}},
	{[]day{{2, 10, 14}}, theme{"San Valentín", []string{"valentine", "heart", "love"}, 6}},
	{[]day{{4, 23, 23}}, theme{"Sant Jordi", []string{"rose", "dragon", "book"}, 6}},
	{[]day{{10, 15, 31}}, theme{"Halloween", []string{"halloween", "pumpkin", "ghost", "bat", "skull"}, 6}},
	{[]day{{11, 1, 2}}, theme{"Todos los Santos", []string{"skull", "ghost", "candle"}, 4}},
	{[]day{{12, 1, 24}}, theme{"Navidad", []string{"christmas", "santa", "snowman", "christmas tree", "reindeer"}, 5}},
	{[]day{{12, 25, 26}}, theme{"Navidad", []string{"christmas", "santa", "gift"}, 8}},
	{[]day{{12, 31, 31}}, theme{"Nochevieja", []string{"fireworks", "party", "champagne"}, 8}},
}

// momentThemes is what suits now: the festivity, the time of day, the day of the week, the season
// and the weather, the more particular ones weighing more.
func momentThemes(now time.Time, w display.Weather) []theme {
	var out []theme
	m, d := int(now.Month()), now.Day()
	for _, f := range festivities {
		for _, when := range f.when {
			if m == when.month && d >= when.from && d <= when.to {
				out = append(out, f.theme)
			}
		}
	}
	if e := easter(now.Year()); !now.Before(e.AddDate(0, 0, -3)) && now.Before(e.AddDate(0, 0, 2)) {
		out = append(out, theme{"Pascua", []string{"easter", "bunny", "egg"}, 6})
	}

	h := now.Hour()*60 + now.Minute()
	switch {
	case h >= 6*60 && h < 10*60+30:
		out = append(out, theme{"Buenos días", []string{"coffee", "sunrise", "breakfast", "bird", "sun"}, 3})
	case h >= 13*60 && h < 15*60+30:
		out = append(out, theme{"Hora de comer", []string{"food", "pizza", "burger", "sushi", "noodles"}, 3})
	case h >= 16*60+30 && h < 18*60+30:
		out = append(out, theme{"Merienda", []string{"cake", "donut", "ice cream", "tea"}, 2})
	case h >= 20*60+30 && h < 23*60:
		out = append(out, theme{"Noche", []string{"moon", "stars", "night", "galaxy"}, 3})
	case h >= 23*60 || h < 6*60:
		out = append(out, theme{"A dormir", []string{"sleep", "moon", "owl", "cat", "zzz"}, 3})
	}

	switch wd := now.Weekday(); {
	case wd == time.Friday && h >= 18*60:
		out = append(out, theme{"Viernes noche", []string{"party", "music", "dance", "disco"}, 2})
	case wd == time.Saturday || wd == time.Sunday:
		out = append(out, theme{"Fin de semana", []string{"game", "music", "dog", "cat", "pixel art"}, 1})
	case wd == time.Monday && h < 12*60:
		out = append(out, theme{"Lunes", []string{"coffee", "cat", "sleepy"}, 1})
	}

	switch {
	case m == 12 || m <= 2:
		out = append(out, theme{"Invierno", []string{"snow", "snowman", "penguin", "winter"}, 1})
	case m <= 5:
		out = append(out, theme{"Primavera", []string{"flower", "butterfly", "spring", "bee"}, 1})
	case m <= 8 || (m == 9 && d < 22):
		out = append(out, theme{"Verano", []string{"beach", "summer", "ice cream", "watermelon", "sun"}, 1})
	default:
		out = append(out, theme{"Otoño", []string{"autumn", "leaf", "mushroom", "rain"}, 1})
	}

	if w.Known {
		switch w.Sky {
		case display.SkyRain, display.SkyStorm:
			out = append(out, theme{"Llueve", []string{"rain", "umbrella", "cloud"}, 3})
		case display.SkySnow:
			out = append(out, theme{"Nieva", []string{"snow", "snowman"}, 4})
		case display.SkyClear:
			if now.Hour() >= 9 && now.Hour() < 19 {
				out = append(out, theme{"Hace sol", []string{"sun", "sunny", "rainbow"}, 1})
			}
		}
	}
	return out
}

// easter is Easter Sunday of year, in the Gregorian calendar.
func easter(year int) time.Time {
	a, b, c := year%19, year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
}
