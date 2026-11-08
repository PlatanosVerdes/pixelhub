// Package lists keeps named lists of gallery pictures, GIFs included, so they stay even if the
// gallery drops them or cannot be reached.
package lists

import (
	"crypto/sha256"
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
	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("there is already a list with that name")
	ErrInvalid  = errors.New("invalid")
)

const maxName = 40

// Favorites is the list that is always there, first, and cannot be renamed or deleted; the page shows
// it under its own name in the reader's language.
const Favorites = "favorites"

// favoriteNames are lists made by hand before Favorites existed, which become it.
var favoriteNames = []string{"favoritos", "favoritas", "favourites", "favs"}

type Picture struct {
	File   string    `json:"file"`
	Name   string    `json:"name"`
	Author string    `json:"author"`
	Likes  int       `json:"likes"`
	Added  time.Time `json:"added"`
}

type List struct {
	Name  string    `json:"name"`
	Items []Picture `json:"items"`
	// A smart list keeps no pictures: it plays what the gallery finds for Search, or shows in Category.
	Search   []string `json:"search,omitempty"`
	Category string   `json:"category,omitempty"`
}

func (l List) Smart() bool { return len(l.Search) > 0 || l.Category != "" }

const maxSearch = 8

type Store struct {
	dir   string
	mu    sync.Mutex
	lists []List
}

// Open reads the lists kept in dir; a missing dir is no lists yet.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}
	data, err := os.ReadFile(s.index())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, s.ensureFavorites()
	case err != nil:
		return nil, fmt.Errorf("read lists: %w", err)
	}
	if err := json.Unmarshal(data, &s.lists); err != nil {
		return nil, fmt.Errorf("read lists: %w", err)
	}
	if err := s.ensureFavorites(); err != nil {
		return nil, err
	}
	return s, nil
}

// ensureFavorites puts Favorites first, taking over a list of favourites made by hand; on a fresh
// install it only lives in memory until something is saved.
func (s *Store) ensureFavorites() error {
	i := s.find(Favorites)
	for _, name := range favoriteNames {
		if i < 0 {
			i = s.find(name)
		}
	}
	next := clone(s.lists)
	if i < 0 {
		next = append([]List{{Name: Favorites, Items: []Picture{}}}, next...)
		if len(s.lists) == 0 {
			s.lists = next
			return nil
		}
	} else {
		fav := next[i]
		fav.Name = Favorites
		next = append([]List{fav}, slices.Delete(next, i, i+1)...)
	}
	if slices.EqualFunc(next, s.lists, func(a, b List) bool { return a.Name == b.Name && len(a.Items) == len(b.Items) }) {
		return nil
	}
	return s.save(next)
}

// Lists is every list in the order made, newest picture first in each; never nil.
func (s *Store) Lists() []List {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.lists)
}

func (s *Store) Create(name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.find(name) >= 0 {
		return fmt.Errorf("%w: %q", ErrExists, name)
	}
	return s.save(append(clone(s.lists), List{Name: name, Items: []Picture{}}))
}

// CreateSmart makes a list that plays what the gallery finds for search, or what is in category.
func (s *Store) CreateSmart(name string, search []string, category string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	var words []string
	for _, w := range search {
		if w = strings.Join(strings.Fields(strings.ToLower(w)), " "); w != "" && !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	if (len(words) == 0) == (category == "") || len(words) > maxSearch {
		return fmt.Errorf("%w: a smart list has 1 to %d words to search for, or a category", ErrInvalid, maxSearch)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.find(name) >= 0 {
		return fmt.Errorf("%w: %q", ErrExists, name)
	}
	return s.save(append(clone(s.lists), List{Name: name, Items: []Picture{}, Search: words, Category: category}))
}

// Smart is the smart list named list, if there is one.
func (s *Store) Smart(list string) (List, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.find(list); i >= 0 && s.lists[i].Smart() {
		return clone(s.lists[i : i+1])[0], true
	}
	return List{}, false
}

func (s *Store) Rename(from, to string) error {
	to, err := cleanName(to)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(from)
	if i < 0 {
		return fmt.Errorf("%w: no list %q", ErrNotFound, from)
	}
	if s.lists[i].Name == Favorites {
		return fmt.Errorf("%w: favorites keeps its name", ErrInvalid)
	}
	if j := s.find(to); j >= 0 && j != i {
		return fmt.Errorf("%w: %q", ErrExists, to)
	}
	next := clone(s.lists)
	next[i].Name = to
	return s.save(next)
}

func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(name)
	if i < 0 {
		return fmt.Errorf("%w: no list %q", ErrNotFound, name)
	}
	if s.lists[i].Name == Favorites {
		return fmt.Errorf("%w: favorites is always there", ErrInvalid)
	}
	gone := s.lists[i].Items
	if err := s.save(slices.Delete(clone(s.lists), i, i+1)); err != nil {
		return err
	}
	for _, p := range gone {
		s.dropOrphan(p.File)
	}
	return nil
}

// Add puts p at the top of list and keeps its GIF; a picture already there moves to the top.
func (s *Store) Add(list string, p Picture, gif []byte) error {
	if p.File == "" || len(gif) == 0 {
		return fmt.Errorf("%w: a picture needs its file and GIF", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(list)
	if i < 0 {
		return fmt.Errorf("%w: no list %q", ErrNotFound, list)
	}
	if s.lists[i].Smart() {
		return fmt.Errorf("%w: %q fills itself from the gallery", ErrInvalid, s.lists[i].Name)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("lists: %w", err)
	}
	if err := atomicfile.Write(s.gifPath(p.File), gif); err != nil {
		return err
	}
	next := clone(s.lists)
	items := next[i].Items
	if j := slices.IndexFunc(items, func(q Picture) bool { return q.File == p.File }); j >= 0 {
		p.Added = items[j].Added
		items = slices.Delete(items, j, j+1)
	}
	if p.Added.IsZero() {
		p.Added = time.Now().UTC().Truncate(time.Second)
	}
	next[i].Items = append([]Picture{p}, items...)
	return s.save(next)
}

func (s *Store) Remove(list, file string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(list)
	if i < 0 {
		return fmt.Errorf("%w: no list %q", ErrNotFound, list)
	}
	next := clone(s.lists)
	j := slices.IndexFunc(next[i].Items, func(q Picture) bool { return q.File == file })
	if j < 0 {
		return fmt.Errorf("%w: not in %q", ErrNotFound, list)
	}
	next[i].Items = slices.Delete(next[i].Items, j, j+1)
	if err := s.save(next); err != nil {
		return err
	}
	s.dropOrphan(file)
	return nil
}

// GIF is the picture kept for file, whichever list holds it.
func (s *Store) GIF(file string) ([]byte, error) {
	s.mu.Lock()
	held := s.holds(file)
	s.mu.Unlock()
	if !held {
		return nil, ErrNotFound
	}
	return os.ReadFile(s.gifPath(file))
}

// dropOrphan removes the GIF once no list holds it; a failure only leaves a file behind.
func (s *Store) dropOrphan(file string) {
	if !s.holds(file) {
		_ = os.Remove(s.gifPath(file))
	}
}

func (s *Store) holds(file string) bool {
	for _, l := range s.lists {
		if slices.ContainsFunc(l.Items, func(p Picture) bool { return p.File == file }) {
			return true
		}
	}
	return false
}

func (s *Store) find(name string) int {
	return slices.IndexFunc(s.lists, func(l List) bool { return strings.EqualFold(l.Name, name) })
}

func (s *Store) save(lists []List) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("lists: %w", err)
	}
	data, err := json.MarshalIndent(lists, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.index(), data); err != nil {
		return err
	}
	s.lists = lists
	return nil
}

func (s *Store) index() string { return filepath.Join(s.dir, "lists.json") }

// gifPath names the GIF after a hash of its gallery file id, which has slashes in it.
func (s *Store) gifPath(file string) string {
	sum := sha256.Sum256([]byte(file))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:12])+".gif")
}

func cleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxName {
		return "", fmt.Errorf("%w: a list name is 1 to %d characters", ErrInvalid, maxName)
	}
	return name, nil
}

func clone(lists []List) []List {
	out := make([]List, len(lists))
	for i, l := range lists {
		out[i] = List{Name: l.Name, Items: append([]Picture{}, l.Items...), Search: slices.Clone(l.Search), Category: l.Category}
	}
	return out
}

// Pick is picture turn of list, in its order, decoded to size; ok is false for an empty or missing list.
func (s *Store) Pick(list string, turn, size int) (display.Content, bool, error) {
	s.mu.Lock()
	i := s.find(list)
	var p Picture
	if i >= 0 && len(s.lists[i].Items) > 0 {
		items := s.lists[i].Items
		p = items[turn%len(items)]
	}
	s.mu.Unlock()
	if p.File == "" {
		return display.Content{}, false, nil
	}
	data, err := os.ReadFile(s.gifPath(p.File))
	if err != nil {
		return display.Content{}, false, fmt.Errorf("list %s: %w", list, err)
	}
	frames, err := sprite.DecodeBytes(data, size)
	if err != nil {
		return display.Content{}, false, fmt.Errorf("list %s: %s: %w", list, p.Name, err)
	}
	return display.Content{Kind: display.Picture, Name: p.Name, Key: p.File, Frames: frames}, true, nil
}
