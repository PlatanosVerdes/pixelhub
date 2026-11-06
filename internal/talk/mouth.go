package talk

import (
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/divoom"
)

const (
	window = 10 * time.Millisecond
	// firstStep is the shortest a mouth shape lasts; it grows until the clip fits the frames the display holds.
	firstStep = 60 * time.Millisecond
	// A silence this long gets a blink, when frames are left over.
	blinkAfter            = 1600 * time.Millisecond
	blinkAt, blinkLasting = 500 * time.Millisecond, 150 * time.Millisecond
	// hold keeps the mouth shut after the clip, so the animation does not start over before it is taken off.
	hold = 60 * time.Second
	// silence is the loudness under which a clip counts as quiet throughout.
	silence = 200
)

type segment struct {
	shape Shape
	dur   time.Duration
}

// Clip is Mosko saying p. lag is how much later the speaker sounds than the display starts the
// animation: a positive one holds the mouth shut first, a negative one holds the sound back.
func Clip(p PCM, lag time.Duration) display.Content {
	budget := divoom.MaxFrames - 1
	if lag > 0 {
		budget--
	}
	segs := mouth(p, budget)
	segs = blinks(segs, budget-len(segs))
	if lag > 0 {
		segs = append([]segment{{Shut, lag}}, segs...)
	}
	segs = append(segs, segment{Shut, hold})
	c := display.Content{
		Kind:   display.Picture,
		Name:   "Mosko",
		Key:    strconv.FormatInt(time.Now().UnixNano(), 36),
		Frames: frames(segs),
		Sound:  p.WAV(),
	}
	if lag < 0 {
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
		out = append(out, display.Frame{Image: Face(s.shape), Delay: end - sent})
		sent = end
	}
	return out
}

// mouth opens the mouth with the loudness of p, in steps that grow until at most most shapes are left.
func mouth(p PCM, most int) []segment {
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
		return []segment{{Shut, p.Duration()}}
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
	for step := firstStep; ; step = step * 5 / 4 {
		k := max(1, int(step/window))
		var segs []segment
		for i := 0; i < len(loud); i += k {
			part := loud[i:min(i+k, len(loud))]
			var sum float64
			for _, v := range part {
				sum += v
			}
			from, to := i*per, min((i+len(part))*per, len(p.Samples))
			dur := time.Duration(to-from) * time.Second / time.Duration(p.Rate)
			s := shape(sum / float64(len(part)))
			if n := len(segs); n > 0 && segs[n-1].shape == s {
				segs[n-1].dur += dur
				continue
			}
			segs = append(segs, segment{s, dur})
		}
		if len(segs) <= most {
			return segs
		}
	}
}

// blinks puts a blink in long silences while room frames are left.
func blinks(segs []segment, room int) []segment {
	out := make([]segment, 0, len(segs))
	for _, s := range segs {
		if s.shape != Shut || s.dur < blinkAfter || room < 2 {
			out = append(out, s)
			continue
		}
		out = append(out, segment{Shut, blinkAt}, segment{Blink, blinkLasting}, segment{Shut, s.dur - blinkAt - blinkLasting})
		room -= 2
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

// Length is how long c keeps the display: its frames before the closing hold.
func Length(c display.Content) time.Duration {
	var d time.Duration
	for _, f := range c.Frames[:max(0, len(c.Frames)-1)] {
		d += f.Delay
	}
	return d + c.SoundAfter
}
