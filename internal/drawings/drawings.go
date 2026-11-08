// Package drawings keeps the animations drawn in the web UI, frame by frame, so they can be edited again.
package drawings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid")
)

const (
	// MaxFrames is what the display plays of an animation.
	MaxFrames = 60
	MinDelay  = 20 * time.Millisecond
	MaxDelay  = 5 * time.Second
	maxName   = 40
)

// Drawing is an animation of Size x Size frames; each frame is its pixels as "rrggbb", row by row.
type Drawing struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	DelayMS int       `json:"delay_ms"`
	Frames  []string  `json:"frames"`
	Updated time.Time `json:"updated"`
}

// Check cleans up the name and reports a drawing the display cannot play.
func (d *Drawing) Check(size int) error {
	d.Name = strings.Join(strings.Fields(d.Name), " ")
	if d.Name == "" || utf8.RuneCountInString(d.Name) > maxName {
		return fmt.Errorf("%w: a name is 1 to %d characters", ErrInvalid, maxName)
	}
	delay := time.Duration(d.DelayMS) * time.Millisecond
	if delay < MinDelay || delay > MaxDelay {
		return fmt.Errorf("%w: delay_ms %d: want %d-%d", ErrInvalid, d.DelayMS, MinDelay.Milliseconds(), MaxDelay.Milliseconds())
	}
	if len(d.Frames) == 0 || len(d.Frames) > MaxFrames {
		return fmt.Errorf("%w: want 1 to %d frames", ErrInvalid, MaxFrames)
	}
	for i, f := range d.Frames {
		d.Frames[i] = strings.ToLower(f)
		if len(f) != size*size*6 {
			return fmt.Errorf("%w: frame %d: want %d pixels as rrggbb", ErrInvalid, i+1, size*size)
		}
		if _, err := hex.DecodeString(f); err != nil {
			return fmt.Errorf("%w: frame %d: %v", ErrInvalid, i+1, err)
		}
	}
	return nil
}

// Images draws the frames; call Check first.
func (d Drawing) Images(size int) []*image.RGBA {
	out := make([]*image.RGBA, 0, len(d.Frames))
	for _, f := range d.Frames {
		raw, _ := hex.DecodeString(f)
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		for p := 0; p*3+2 < len(raw); p++ {
			img.SetRGBA(p%size, p/size, color.RGBA{raw[p*3], raw[p*3+1], raw[p*3+2], 255})
		}
		out = append(out, img)
	}
	return out
}

func (d Drawing) Delay() time.Duration { return time.Duration(d.DelayMS) * time.Millisecond }

type Store struct {
	path     string
	size     int
	mu       sync.Mutex
	drawings []Drawing
}

// Open reads the drawings kept in dir; a missing dir is none yet.
func Open(dir string, size int) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "drawings.json"), size: size, drawings: []Drawing{}}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read drawings: %w", err)
	}
	if err := json.Unmarshal(data, &s.drawings); err != nil {
		return nil, fmt.Errorf("read drawings: %w", err)
	}
	return s, nil
}

// List is every drawing, the last changed first; never nil.
func (s *Store) List() []Drawing {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.drawings)
}

func (s *Store) Get(id string) (Drawing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return Drawing{}, fmt.Errorf("%w: no drawing %q", ErrNotFound, id)
	}
	return s.drawings[i], nil
}

// Save keeps d, as a new drawing when it has no ID, and answers it as kept.
func (s *Store) Save(d Drawing) (Drawing, error) {
	if err := d.Check(s.size); err != nil {
		return Drawing{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := slices.Clone(s.drawings)
	if d.ID == "" {
		d.ID = newID()
	} else if i := s.find(d.ID); i >= 0 {
		next = slices.Delete(next, i, i+1)
	} else {
		return Drawing{}, fmt.Errorf("%w: no drawing %q", ErrNotFound, d.ID)
	}
	d.Updated = time.Now().UTC().Truncate(time.Second)
	if err := s.save(append([]Drawing{d}, next...)); err != nil {
		return Drawing{}, err
	}
	return d, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return fmt.Errorf("%w: no drawing %q", ErrNotFound, id)
	}
	return s.save(slices.Delete(slices.Clone(s.drawings), i, i+1))
}

func (s *Store) find(id string) int {
	return slices.IndexFunc(s.drawings, func(d Drawing) bool { return d.ID == id })
}

func (s *Store) save(drawings []Drawing) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("drawings: %w", err)
	}
	data, err := json.Marshal(drawings)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.path, data); err != nil {
		return err
	}
	s.drawings = drawings
	return nil
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
