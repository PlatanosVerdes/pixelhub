package talk

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

// NoiseKinds are the noises Mosko can make out of nowhere.
var NoiseKinds = []string{"fart", "burp", "scream", "sound"}

var ErrNoNoise = errors.New("no such noise")

// Noise is a made-up sound and how Mosko looks while making it.
type Noise struct {
	Kind  string
	PCM   PCM
	Spans []Span
	// Mouth opens the mouth with the sound; a fart keeps it shut.
	Mouth bool
}

// MakeNoise synthesises a different noise of kind each time.
func MakeNoise(kind string) (Noise, error) {
	n := Noise{Kind: kind, Mouth: true}
	var after Mood
	var afterFor time.Duration
	switch kind {
	case "fart":
		n.PCM, n.Mouth = fart(), false
		n.Spans = []Span{{Thinking, 0, n.PCM.Duration()}}
		after, afterFor = Laughing, 1500*time.Millisecond
	case "burp":
		n.PCM = burp()
		after, afterFor = Laughing, 1200*time.Millisecond
	case "scream":
		n.PCM = scream()
		n.Spans = []Span{{Excited, 0, n.PCM.Duration()}}
	case "sound":
		n.PCM = little()
		n.Spans = []Span{{[]Mood{Excited, Asking, Laughing}[rand.IntN(3)], 0, n.PCM.Duration()}}
	default:
		return Noise{}, fmt.Errorf("%w: %q, want one of %v", ErrNoNoise, kind, NoiseKinds)
	}
	if afterFor > 0 {
		end := n.PCM.Duration()
		n.PCM = n.PCM.withTrailingSilence(afterFor)
		n.Spans = append(n.Spans, Span{after, end, end + afterFor})
	}
	return n, nil
}

// Clip is Mosko making the noise on the display, like Clip for speech.
func (n Noise) Clip(lag time.Duration) (display.Content, []Pose) {
	return clip(n.PCM, n.Spans, lag, n.Mouth)
}

type synth struct {
	out   []float64
	phase float64
}

func newSynth(d time.Duration) *synth {
	return &synth{out: make([]float64, int(d.Seconds()*Rate))}
}

func (s *synth) at(i int) float64 { return float64(i) / Rate }

// advance moves the phase by one sample at f Hz and answers it in 0-1.
func (s *synth) advance(f float64) float64 {
	s.phase += f / Rate
	s.phase -= math.Floor(s.phase)
	return s.phase
}

// biquad is a band-pass filter, how a mouth shapes a buzz into a vowel.
type biquad struct{ b0, b2, a1, a2, x1, x2, y1, y2 float64 }

func bandPass(f, q float64) *biquad {
	w := 2 * math.Pi * f / Rate
	alpha := math.Sin(w) / (2 * q)
	a0 := 1 + alpha
	return &biquad{b0: alpha / a0, b2: -alpha / a0, a1: -2 * math.Cos(w) / a0, a2: (1 - alpha) / a0}
}

func (b *biquad) step(x float64) float64 {
	y := b.b0*x + b.b2*b.x2 - b.a1*b.y1 - b.a2*b.y2
	b.x2, b.x1, b.y2, b.y1 = b.x1, x, b.y1, y
	return y
}

// vowel runs x through formants, each a frequency with its weight.
func vowel(x []float64, formants ...[3]float64) []float64 {
	out := make([]float64, len(x))
	for _, f := range formants {
		bp := bandPass(f[0], f[1])
		for i, v := range x {
			out[i] += f[2] * bp.step(v)
		}
	}
	return out
}

func lowPass(x []float64, f float64) {
	k := 1 - math.Exp(-2*math.Pi*f/Rate)
	var y float64
	for i, v := range x {
		y += k * (v - y)
		x[i] = y
	}
}

// envelope rises in attack and falls in release.
func envelope(i, n int, attack, release time.Duration) float64 {
	a, r := int(attack.Seconds()*Rate), int(release.Seconds()*Rate)
	switch {
	case i < a:
		return float64(i) / float64(a)
	case i > n-r:
		return math.Max(0, float64(n-i)/float64(r))
	}
	return 1
}

func between(lo, hi float64) float64 { return lo + rand.Float64()*(hi-lo) }

func seconds(lo, hi float64) time.Duration {
	return time.Duration(between(lo, hi) * float64(time.Second))
}

// pcm scales x to a loud but safe peak.
func pcm(x []float64) PCM {
	var peak float64
	for _, v := range x {
		peak = math.Max(peak, math.Abs(v))
	}
	out := PCM{Rate: Rate, Samples: make([]int16, len(x))}
	if peak == 0 {
		return out
	}
	for i, v := range x {
		out.Samples[i] = int16(v / peak * 26000)
	}
	return out
}

// pulses is a flapping buzz: a short bump each period, its pitch wandering by jitter, each bump as
// loud as rough lets it.
func pulses(s *synth, pitch func(t float64) float64, jitter, rough float64) []float64 {
	x := make([]float64, len(s.out))
	gain, wobble := 1.0, 1.0
	for i := range x {
		before := s.phase
		ph := s.advance(pitch(s.at(i)) * wobble)
		if ph < before {
			gain = 1 - rough*rand.Float64()
			wobble = 1 + jitter*(rand.Float64()*2-1)
		}
		if ph < 0.3 {
			x[i] = gain * math.Sin(math.Pi*ph/0.3)
		}
	}
	return x
}

func fart() PCM {
	d := seconds(0.4, 1.6)
	if rand.IntN(4) == 0 {
		d = seconds(0.15, 0.35)
	}
	s := newSynth(d)
	n := len(s.out)
	base := between(70, 150)
	if d < 400*time.Millisecond {
		base = between(180, 300)
	}
	flutter := between(6, 14)
	total := d.Seconds()
	x := pulses(s, func(t float64) float64 {
		end := math.Max(0, t/total-0.6) / 0.4
		return base * (1 + 0.12*math.Sin(2*math.Pi*flutter*t)) * (1 - 0.35*end)
	}, 0.08, 0.5)
	for i := range x {
		x[i] += 0.12 * (rand.Float64()*2 - 1)
	}
	lowPass(x, between(500, 900))
	gaps := rand.IntN(3)
	for i := range x {
		x[i] *= envelope(i, n, 15*time.Millisecond, d/3)
	}
	for range gaps {
		at := n/2 + rand.IntN(n/2)
		for j := at; j < min(n, at+Rate*3/100); j++ {
			x[j] *= 0.05
		}
	}
	return pcm(x)
}

func burp() PCM {
	d := seconds(0.6, 1.4)
	s := newSynth(d)
	n := len(s.out)
	base := between(55, 85)
	x := pulses(s, func(t float64) float64 { return base * (1 - 0.15*t/d.Seconds()) }, 0.18, 0.7)
	for i := range x {
		x[i] += 0.08 * (rand.Float64()*2 - 1)
	}
	open := between(500, 700)
	y := vowel(x, [3]float64{open, 4, 1}, [3]float64{open * 1.7, 6, 0.6}, [3]float64{2400, 8, 0.15})
	lowPass(y, 1800)
	for i := range y {
		swell := math.Sin(math.Pi * math.Min(1, float64(i)/float64(n)*1.2))
		y[i] *= envelope(i, n, 30*time.Millisecond, 120*time.Millisecond) * (0.4 + 0.6*swell)
	}
	return pcm(y)
}

func scream() PCM {
	d := seconds(0.8, 1.6)
	s := newSynth(d)
	n := len(s.out)
	low, high := between(380, 520), between(750, 1050)
	total := d.Seconds()
	falling := rand.IntN(2) == 0
	x := make([]float64, n)
	for i := range x {
		t := s.at(i)
		rise := math.Min(1, t/(0.25*total))
		if falling {
			rise = 1 - 0.5*math.Max(0, t/total-0.3)
		}
		f := (low + (high-low)*rise) * (1 + 0.03*math.Sin(2*math.Pi*6*t)) * (1 + 0.01*(rand.Float64()*2-1))
		x[i] = 2*s.advance(f) - 1 + 0.15*(rand.Float64()*2-1)
	}
	y := vowel(x, [3]float64{900, 4, 1}, [3]float64{1350, 5, 0.8}, [3]float64{2900, 7, 0.35})
	for i := range y {
		y[i] *= envelope(i, n, 50*time.Millisecond, 200*time.Millisecond)
	}
	return pcm(y)
}

// little is one of a few cartoon sounds: a boing, a bloop, a whistle or a beep-boop.
func little() PCM {
	var x []float64
	switch rand.IntN(4) {
	case 0:
		s := newSynth(900 * time.Millisecond)
		base := between(160, 240)
		for i := range s.out {
			t := s.at(i)
			f := base * (1 + 0.5*math.Exp(-3*t)*math.Sin(2*math.Pi*13*t))
			s.out[i] = math.Sin(2*math.Pi*s.advance(f)) * math.Exp(-2.5*t)
		}
		x = s.out
	case 1:
		for range 1 + rand.IntN(3) {
			s := newSynth(160 * time.Millisecond)
			from, to := between(250, 400), between(800, 1200)
			for i := range s.out {
				t := s.at(i) / 0.16
				s.out[i] = math.Sin(2*math.Pi*s.advance(from+(to-from)*t*t)) * math.Sin(math.Pi*t)
			}
			x = append(append(x, s.out...), make([]float64, Rate*6/100)...)
		}
	case 2:
		s := newSynth(seconds(0.6, 0.9))
		n := len(s.out)
		a, b, c := between(800, 1000), between(1600, 2000), between(600, 800)
		for i := range s.out {
			t := float64(i) / float64(n)
			f := a + (b-a)*math.Min(1, t*2)
			if t > 0.5 {
				f = b + (c-b)*(t-0.5)*2
			}
			s.out[i] = math.Sin(2*math.Pi*s.advance(f*(1+0.02*math.Sin(2*math.Pi*7*s.at(i))))) * envelope(i, n, 20*time.Millisecond, 80*time.Millisecond)
		}
		x = s.out
	default:
		notes := []float64{523, 587, 659, 784, 880, 1047}
		for range 3 + rand.IntN(3) {
			s := newSynth(90 * time.Millisecond)
			f := notes[rand.IntN(len(notes))]
			for i := range s.out {
				sq := 1.0
				if s.advance(f) > 0.5 {
					sq = -1
				}
				s.out[i] = 0.6 * sq * envelope(i, len(s.out), 5*time.Millisecond, 30*time.Millisecond)
			}
			x = append(append(x, s.out...), make([]float64, Rate*3/100)...)
		}
		lowPass(x, 3000)
	}
	return pcm(x)
}
