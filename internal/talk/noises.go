package talk

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

// NoiseKinds are the noises Mosko can make out of nowhere.
var NoiseKinds = []string{"fart", "burp", "scream", "sound"}

var ErrNoNoise = errors.New("no such noise")

//go:embed noises/*.mp3
var noiseFiles embed.FS

// Noise is a recorded noise and how Mosko looks while making it.
type Noise struct {
	Kind  string
	File  string
	PCM   PCM
	Spans []Span
	// Mouth opens the mouth with the sound; a fart keeps it shut.
	Mouth bool
}

// Noises picks recordings at random, never the same one twice in a row, and keeps them decoded.
type Noises struct {
	Decoder interface {
		Decode(ctx context.Context, audio []byte, pitch float64, effect string) (PCM, error)
	}
	mu      sync.Mutex
	decoded map[string]PCM
	last    map[string]string
}

// NoiseFiles are the recordings of kind.
func NoiseFiles(kind string) []string {
	files, _ := fs.Glob(noiseFiles, "noises/"+kind+"-*.mp3")
	return files
}

func (n *Noises) pick(kind string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	files := NoiseFiles(kind)
	if len(files) > 1 {
		files = slices.DeleteFunc(files, func(f string) bool { return f == n.last[kind] })
	}
	f := files[rand.IntN(len(files))]
	if n.last == nil {
		n.last = map[string]string{}
	}
	n.last[kind] = f
	return f
}

func (n *Noises) pcm(ctx context.Context, file string) (PCM, error) {
	n.mu.Lock()
	p, ok := n.decoded[file]
	n.mu.Unlock()
	if ok {
		return p, nil
	}
	data, err := noiseFiles.ReadFile(file)
	if err != nil {
		return PCM{}, err
	}
	if p, err = n.Decoder.Decode(ctx, data, 1, ""); err != nil {
		return PCM{}, fmt.Errorf("%s: %w", file, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.decoded == nil {
		n.decoded = map[string]PCM{}
	}
	n.decoded[file] = p
	return p, nil
}

// Make is a noise of kind, one of its recordings with the faces that go with it.
func (n *Noises) Make(ctx context.Context, kind string) (Noise, error) {
	if !slices.Contains(NoiseKinds, kind) {
		return Noise{}, fmt.Errorf("%w: %q, want one of %v", ErrNoNoise, kind, NoiseKinds)
	}
	file := n.pick(kind)
	p, err := n.pcm(ctx, file)
	if err != nil {
		return Noise{}, err
	}
	out := Noise{Kind: kind, File: strings.TrimPrefix(file, "noises/"), PCM: p, Mouth: true}
	var after Mood
	var afterFor time.Duration
	switch kind {
	case "fart":
		out.Mouth = false
		out.Spans = []Span{{Thinking, 0, p.Duration()}}
		after, afterFor = Laughing, 1500*time.Millisecond
	case "burp":
		after, afterFor = Laughing, 1200*time.Millisecond
	case "scream":
		out.Spans = []Span{{Excited, 0, p.Duration()}}
	case "sound":
		out.Spans = []Span{{[]Mood{Excited, Asking, Laughing}[rand.IntN(3)], 0, p.Duration()}}
	}
	if afterFor > 0 {
		end := p.Duration()
		// The kept recording is shared, so the silence goes on a copy.
		out.PCM = PCM{Rate: p.Rate, Samples: slices.Clip(p.Samples)}.withTrailingSilence(afterFor)
		out.Spans = append(out.Spans, Span{after, end, end + afterFor})
	}
	return out, nil
}

// Clip is Mosko making the noise on the display, like Clip for speech.
func (n Noise) Clip(lag time.Duration) (display.Content, []Pose) {
	return clip(n.PCM, n.Spans, lag, n.Mouth)
}
