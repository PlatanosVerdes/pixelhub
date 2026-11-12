package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
)

type Store struct {
	path string
}

// Open loads the state at path. A missing file gives the defaults; a file that does not parse or
// validate is renamed aside so the service still starts, and the defaults are used.
func Open(path string, log *slog.Logger) (*Store, State, error) {
	store := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return store, Defaults(), nil
	}
	if err != nil {
		return nil, State{}, fmt.Errorf("read state: %w", err)
	}
	s, err := Decode(data)
	if err == nil {
		return store, s, nil
	}
	aside := fmt.Sprintf("%s.corrupt-%s", path, time.Now().Format("20060102-150405"))
	if renameErr := os.Rename(path, aside); renameErr != nil {
		return nil, State{}, fmt.Errorf("move the unreadable state aside: %w", renameErr)
	}
	log.Error("state unreadable, starting from defaults", "err", err, "kept_as", aside)
	return store, Defaults(), nil
}

func Decode(data []byte) (State, error) {
	var s State
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	return s.Clone(), s.Validate()
}

func (st *Store) Save(s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return atomicfile.Write(st.path, data)
}

// Clone deep-copies s, so a change can be checked before it replaces the live state. Lists come out
// empty rather than nil, so the JSON shows [] instead of null.
func (s State) Clone() State {
	out := s
	out.Schedule = append([]Slot{}, s.Schedule...)
	out.Music.Devices = slices.Clone(s.Music.Devices)
	out.Volume = clonePtr(s.Volume)
	out.Talk.LagMS = clonePtr(s.Talk.LagMS)
	out.Channels = make([]Channel, len(s.Channels))
	for i, c := range s.Channels {
		c.Sources = append([]string{}, c.Sources...)
		c.Scenes = append([]Scene{}, c.Scenes...)
		out.Channels[i] = c
	}
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
