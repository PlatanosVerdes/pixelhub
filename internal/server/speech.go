package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

// QuickPhrases are what the Mosko tab says with one tap; they are read once and kept on disk.
var QuickPhrases = []string{"¡Hola!", "Sí.", "No.", "Vale.", "¡Gracias!", "¡Adiós!", "¿Qué tal?", "¡Buenos días!", "Buenas noches...",
	"¡A comer!", "¡Bravo!", "Jajaja", "¿En serio?", "Ni idea...", "¡Cuidado!", "¡Te quiero!", "¡Socorro!", "Mmm... déjame pensar."}

const (
	keptLines = 256
	// keptFiles is how many read sentences stay on disk; the oldest go first.
	keptFiles = 400
)

// keptVoice remembers what the voice read, a sentence at a time, in memory and in dir when set, so a
// repeated phrase needs no speech run, across restarts too. One run at a time, shared by requests and warm-ups.
type keptVoice struct {
	Voice
	dir  string
	one  chan struct{}
	mu   sync.Mutex
	kept map[string]talk.PCM
}

func newKeptVoice(v Voice, dir string) *keptVoice {
	return &keptVoice{Voice: v, dir: dir, one: make(chan struct{}, 1), kept: map[string]talk.PCM{}}
}

func (k *keptVoice) path(text string) string {
	sum := sha256.Sum256([]byte(text))
	return filepath.Join(k.dir, hex.EncodeToString(sum[:12])+".pcm")
}

func (k *keptVoice) cached(text string) (talk.PCM, bool) {
	k.mu.Lock()
	p, ok := k.kept[text]
	k.mu.Unlock()
	if ok || k.dir == "" {
		return p, ok
	}
	data, err := os.ReadFile(k.path(text))
	if err != nil || len(data) < 4 || len(data)%2 != 0 {
		return talk.PCM{}, false
	}
	p = talk.PCM{Rate: int(binary.LittleEndian.Uint32(data)), Samples: make([]int16, (len(data)-4)/2)}
	for i := range p.Samples {
		p.Samples[i] = int16(binary.LittleEndian.Uint16(data[4+2*i:]))
	}
	k.remember(text, p)
	return p, true
}

func (k *keptVoice) remember(text string, p talk.PCM) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.kept) >= keptLines {
		clear(k.kept)
	}
	k.kept[text] = p
}

// store writes a read sentence as its rate (uint32 LE) and its samples (int16 LE); a failure only costs a reread.
func (k *keptVoice) store(text string, p talk.PCM) {
	if k.dir == "" {
		return
	}
	data := make([]byte, 4+2*len(p.Samples))
	binary.LittleEndian.PutUint32(data, uint32(p.Rate))
	for i, v := range p.Samples {
		binary.LittleEndian.PutUint16(data[4+2*i:], uint16(v))
	}
	if err := os.MkdirAll(k.dir, 0o700); err == nil {
		err = atomicfile.Write(k.path(text), data)
		if err != nil {
			slog.Warn("speech not kept", "err", err)
		}
	}
}

func (k *keptVoice) Say(ctx context.Context, text string) (talk.PCM, error) {
	if p, ok := k.cached(text); ok {
		return p, nil
	}
	select {
	case k.one <- struct{}{}:
	case <-ctx.Done():
		return talk.PCM{}, ctx.Err()
	}
	defer func() { <-k.one }()
	if p, ok := k.cached(text); ok {
		return p, nil
	}
	p, err := k.Voice.Say(ctx, text)
	if err != nil {
		return talk.PCM{}, err
	}
	k.remember(text, p)
	k.store(text, p)
	return p, nil
}

// warm reads texts ahead, in the background, so they come out at once when asked for.
func (k *keptVoice) warm(ctx context.Context, texts []string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, text := range texts {
		for _, l := range talk.Script(text) {
			if _, err := k.Say(ctx, l.Text); err != nil {
				slog.Warn("phrase not read ahead", "text", l.Text, "err", err)
				return
			}
		}
	}
}

// prune keeps the newest keptFiles sentences on disk.
func (k *keptVoice) prune() {
	if k.dir == "" {
		return
	}
	entries, err := os.ReadDir(k.dir)
	if err != nil || len(entries) <= keptFiles {
		return
	}
	type file struct {
		name string
		at   time.Time
	}
	var files []file
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			files = append(files, file{e.Name(), info.ModTime()})
		}
	}
	slices.SortFunc(files, func(a, b file) int { return b.at.Compare(a.at) })
	for _, f := range files[min(len(files), keptFiles):] {
		_ = os.Remove(filepath.Join(k.dir, f.name))
	}
}

// WarmSpeech reads the quick phrases at start, those not kept from before.
func (s *Server) WarmSpeech(ctx context.Context) {
	if s.voice == nil {
		return
	}
	s.voice.prune()
	s.voice.warm(ctx, QuickPhrases)
}

func (s *Server) phrases(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]string{"phrases": QuickPhrases})
}

// warmUp reads the quick phrases again, for a voice that was away at start.
func (s *Server) warmUp(w http.ResponseWriter, _ *http.Request) {
	if s.voice == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("no speech server set up"))
		return
	}
	go s.voice.warm(context.Background(), QuickPhrases)
	writeJSON(w, http.StatusAccepted, map[string]int{"warming": len(QuickPhrases)})
}
