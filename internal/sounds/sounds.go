// Package sounds keeps Mosko's soundboard: recordings and phrases he says at a tap.
package sounds

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	maxName = 30
	maxText = 400
)

// Sound is a recording (Audio) or a phrase read aloud (Text).
type Sound struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Text  string    `json:"text,omitempty"`
	Audio bool      `json:"audio"`
	Added time.Time `json:"added"`
}

type Store struct {
	dir    string
	mu     sync.Mutex
	sounds []Sound
}

// Open reads the sounds kept in dir; a missing dir is none yet.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, sounds: []Sound{}}
	data, err := os.ReadFile(s.index())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read sounds: %w", err)
	}
	if err := json.Unmarshal(data, &s.sounds); err != nil {
		return nil, fmt.Errorf("read sounds: %w", err)
	}
	return s, nil
}

// List is every sound in the order added; never nil.
func (s *Store) List() []Sound {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.sounds)
}

// AddText keeps a phrase.
func (s *Store) AddText(name, text string) (Sound, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxText {
		return Sound{}, fmt.Errorf("%w: a phrase is 1 to %d characters", ErrInvalid, maxText)
	}
	return s.add(Sound{Name: name, Text: text}, nil)
}

// AddAudio keeps a recording, in whatever format it came.
func (s *Store) AddAudio(name string, audio []byte) (Sound, error) {
	if len(audio) == 0 {
		return Sound{}, fmt.Errorf("%w: empty recording", ErrInvalid)
	}
	return s.add(Sound{Name: name, Audio: true}, audio)
}

func (s *Store) add(snd Sound, audio []byte) (Sound, error) {
	snd.Name = strings.Join(strings.Fields(snd.Name), " ")
	if snd.Name == "" || utf8.RuneCountInString(snd.Name) > maxName {
		return Sound{}, fmt.Errorf("%w: a name is 1 to %d characters", ErrInvalid, maxName)
	}
	snd.ID = newID()
	snd.Added = time.Now().UTC().Truncate(time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Sound{}, fmt.Errorf("sounds: %w", err)
	}
	if audio != nil {
		if err := atomicfile.Write(s.audioPath(snd.ID), audio); err != nil {
			return Sound{}, err
		}
	}
	if err := s.save(append(slices.Clone(s.sounds), snd)); err != nil {
		_ = os.Remove(s.audioPath(snd.ID))
		return Sound{}, err
	}
	return snd, nil
}

// Get is a sound and, for a recording, its audio.
func (s *Store) Get(id string) (Sound, []byte, error) {
	s.mu.Lock()
	i := s.find(id)
	var snd Sound
	if i >= 0 {
		snd = s.sounds[i]
	}
	s.mu.Unlock()
	if i < 0 {
		return Sound{}, nil, fmt.Errorf("%w: no sound %q", ErrNotFound, id)
	}
	if !snd.Audio {
		return snd, nil, nil
	}
	audio, err := os.ReadFile(s.audioPath(id))
	if err != nil {
		return Sound{}, nil, fmt.Errorf("sound %s: %w", snd.Name, err)
	}
	return snd, audio, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return fmt.Errorf("%w: no sound %q", ErrNotFound, id)
	}
	if err := s.save(slices.Delete(slices.Clone(s.sounds), i, i+1)); err != nil {
		return err
	}
	_ = os.Remove(s.audioPath(id))
	return nil
}

func (s *Store) find(id string) int {
	return slices.IndexFunc(s.sounds, func(snd Sound) bool { return snd.ID == id })
}

func (s *Store) save(sounds []Sound) error {
	data, err := json.MarshalIndent(sounds, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.index(), data); err != nil {
		return err
	}
	s.sounds = sounds
	return nil
}

func (s *Store) index() string { return filepath.Join(s.dir, "sounds.json") }

func (s *Store) audioPath(id string) string { return filepath.Join(s.dir, id+".audio") }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
