// Package favorites keeps the gallery pictures someone liked, GIF included, so they stay even if
// the gallery drops them or cannot be reached.
package favorites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
)

var ErrNotFound = errors.New("not among the favourites")

type Favorite struct {
	File   string    `json:"file"`
	Name   string    `json:"name"`
	Author string    `json:"author"`
	Likes  int       `json:"likes"`
	Added  time.Time `json:"added"`
}

type Store struct {
	dir  string
	mu   sync.Mutex
	list []Favorite
}

// Open reads the favourites kept in dir; a missing dir is an empty list.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}
	data, err := os.ReadFile(s.index())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read favourites: %w", err)
	}
	if err := json.Unmarshal(data, &s.list); err != nil {
		return nil, fmt.Errorf("read favourites: %w", err)
	}
	return s, nil
}

// List is newest first.
func (s *Store) List() []Favorite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

func (s *Store) Has(file string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.indexOf(file) >= 0
}

// Add keeps f and its GIF; adding one already there refreshes its details.
func (s *Store) Add(f Favorite, gif []byte) error {
	if f.File == "" || len(gif) == 0 {
		return errors.New("a favourite needs its file and picture")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("favourites: %w", err)
	}
	if err := atomicfile.Write(s.gifPath(f.File), gif); err != nil {
		return err
	}
	next := slices.Clone(s.list)
	if i := s.indexOf(f.File); i >= 0 {
		f.Added = next[i].Added
		next = slices.Delete(next, i, i+1)
	}
	if f.Added.IsZero() {
		f.Added = time.Now().UTC().Truncate(time.Second)
	}
	return s.save(append([]Favorite{f}, next...))
}

func (s *Store) Remove(file string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(file)
	if i < 0 {
		return ErrNotFound
	}
	if err := s.save(slices.Delete(slices.Clone(s.list), i, i+1)); err != nil {
		return err
	}
	if err := os.Remove(s.gifPath(file)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("favourites: %w", err)
	}
	return nil
}

// GIF is the picture kept for file.
func (s *Store) GIF(file string) ([]byte, error) {
	if !s.Has(file) {
		return nil, ErrNotFound
	}
	return os.ReadFile(s.gifPath(file))
}

func (s *Store) save(list []Favorite) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.index(), data); err != nil {
		return err
	}
	s.list = list
	return nil
}

func (s *Store) indexOf(file string) int {
	return slices.IndexFunc(s.list, func(f Favorite) bool { return f.File == file })
}

func (s *Store) index() string { return filepath.Join(s.dir, "favorites.json") }

// gifPath names the GIF after a hash of its gallery file id, which has slashes in it.
func (s *Store) gifPath(file string) string {
	sum := sha256.Sum256([]byte(file))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:12])+".gif")
}
