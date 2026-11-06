package talk

import (
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	window = 10 * time.Millisecond
	// step is the shortest a mouth shape lasts.
	step = 60 * time.Millisecond
	// A silence this long gets a blink.
	blinkAfter            = 1600 * time.Millisecond
	blinkAt, blinkLasting = 500 * time.Millisecond, 150 * time.Millisecond
	// wake is silence put before the voice: the speaker drops the first moment while its Bluetooth stream starts.
	wake = 800 * time.Millisecond
	// silence is the loudness under which a clip counts as quiet throughout.
	silence = 200
)

type segment struct {
	shape Shape
	mood  Mood
	dur   time.Duration
}

// Clip is Mosko saying p, one still face per change of mouth or mood, shown in time with the sound
// by the bridge. spans set the moods, Calm elsewhere. lag is how much later the speaker sounds than
// the display paints: a positive one holds the faces back, a negative one the sound.
func Clip(p PCM, spans []Span, lag time.Duration) display.Content {
	p = p.withSilence(wake)
	spans = slices.Clone(spans)
	for i := range spans {
		spans[i].From += wake
		spans[i].To += wake
	}
	c := display.Content{
		Kind:   display.Picture,
		Name:   "Mosko",
		Key:    strconv.FormatInt(time.Now().UnixNano(), 36),
		Frames: frames(rest(blinks(moods(mouth(p), spans)))),
		Sound:  p.WAV(),
	}
	if lag > 0 {
		c.FramesAfter = lag
	} else {
		c.SoundAfter = -lag
	}
	return c
}

// frames rounds each segment to whole milliseconds on the running total, so the mouth never drifts from the sound.
func frames(segs []segment) []display.Frame {
	out := make([]display.Frame, 0, len(segs))
	var at, sent time.Duration
	for _, s := range segs {
		at += s.dur
		end := at.Round(time.Millisecond)
		out = append(out, display.Frame{Image: Face(s.shape, s.mood), Delay: end - sent})
		sent = end
	}
	return out
}

// mouth opens the mouth with the loudness of p.
func mouth(p PCM) []segment {
	per := max(1, p.Rate*int(window)/int(time.Second))
	var loud []float64
	for i := 0; i < len(p.Samples); i += per {
		loud = append(loud, rms(p.Samples[i:min(i+per, len(p.Samples))]))
	}
	if len(loud) == 0 {
		return nil
	}
	sorted := slices.Sorted(slices.Values(loud))
	ref := sorted[len(sorted)*95/100]
	if ref < silence {
		return []segment{{Shut, Calm, p.Duration()}}
	}
	gate := max(sorted[len(sorted)/10]*3, ref*0.12, silence)
	shape := func(v float64) Shape {
		switch {
		case v < gate:
			return Shut
		case v < ref*0.35:
			return Small
		case v < ref*0.7:
			return Half
		}
		return Wide
	}
	k := int(step / window)
	var segs []segment
	for i := 0; i < len(loud); i += k {
		part := loud[i:min(i+k, len(loud))]
		var sum float64
		for _, v := range part {
			sum += v
		}
		from, to := i*per, min((i+len(part))*per, len(p.Samples))
		segs = add(segs, segment{shape(sum / float64(len(part))), Calm, time.Duration(to-from) * time.Second / time.Duration(p.Rate)})
	}
	return segs
}

func add(segs []segment, s segment) []segment {
	if n := len(segs); n > 0 && segs[n-1].shape == s.shape && segs[n-1].mood == s.mood {
		segs[n-1].dur += s.dur
		return segs
	}
	return append(segs, s)
}

// moods cuts the mouth shapes where the spans start and end, and gives each piece its span's mood.
func moods(segs []segment, spans []Span) []segment {
	var out []segment
	var at time.Duration
	for _, s := range segs {
		end := at + s.dur
		for at < end {
			mood, until := Calm, end
			for _, sp := range spans {
				switch {
				case sp.From <= at && at < sp.To:
					mood, until = sp.Mood, min(until, sp.To)
				case at < sp.From && sp.From < until:
					until = sp.From
				}
			}
			out = add(out, segment{s.shape, mood, until - at})
			at = until
		}
	}
	return out
}

// rest ends on the mouth shut, which stays on the display after the sound.
func rest(segs []segment) []segment {
	if n := len(segs); n == 0 || segs[n-1].shape != Shut {
		segs = append(segs, segment{Shut, Calm, 0})
	}
	return segs
}

// blinks puts a blink in long silences.
func blinks(segs []segment) []segment {
	out := make([]segment, 0, len(segs))
	for _, s := range segs {
		if s.shape != Shut || s.mood != Calm || s.dur < blinkAfter {
			out = append(out, s)
			continue
		}
		out = append(out, segment{Shut, Calm, blinkAt}, segment{Blink, Calm, blinkLasting}, segment{Shut, Calm, s.dur - blinkAt - blinkLasting})
	}
	return out
}

func rms(s []int16) float64 {
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

// Length is how long c keeps talking, from the bridge taking it to the last face or the end of the sound.
func Length(c display.Content) time.Duration {
	var d time.Duration
	for _, f := range c.Frames {
		d += f.Delay
	}
	return max(d+c.FramesAfter, d+c.SoundAfter)
}
