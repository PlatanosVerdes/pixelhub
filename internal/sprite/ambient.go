package sprite

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	MaxPictureBytes = 1 << 20
	// A small file can still declare huge dimensions; decoding it would exhaust the Pi's memory.
	maxPictureSide = 512
)

var (
	ErrBadPicture = errors.New("bad picture")
	ErrBadName    = errors.New("bad file name")
	ErrTooBig     = errors.New("picture too big")
	ErrExists     = errors.New("picture already exists")

	unsafeRunes = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
)

// Ambient keeps each channel's pictures in Dir/<channel>/; a channel with none uses Dir itself.
// The folders are read again on every pick, so files can come and go.
type Ambient struct {
	Dir  string
	Size int
}

func (a Ambient) Pick(channel string, turn int) (display.Content, bool, error) {
	dir, err := a.channelDir(channel)
	if err != nil {
		return display.Content{}, false, err
	}
	files, err := pictureFiles(dir)
	if err == nil && len(files) == 0 {
		files, err = pictureFiles(a.Dir)
	}
	if err != nil || len(files) == 0 {
		return display.Content{}, false, err
	}
	path := files[turn%len(files)]
	frames, err := Decode(path, a.Size)
	if err != nil {
		return display.Content{}, false, err
	}
	// The folder is part of the name so the same file name in two channels still counts as a change.
	name, _ := filepath.Rel(a.Dir, path)
	return display.Content{Kind: display.Picture, Name: name, Frames: frames}, true, nil
}

// List names the channel's own pictures, without the shared fallback ones.
func (a Ambient) List(channel string) ([]string, error) {
	dir, err := a.channelDir(channel)
	if err != nil {
		return nil, err
	}
	files, err := pictureFiles(dir)
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	return names, err
}

// Add checks that data decodes as a GIF or PNG and saves it under a cleaned-up name, which it returns.
func (a Ambient) Add(channel, name string, data []byte) (string, error) {
	if len(data) > MaxPictureBytes {
		return "", fmt.Errorf("%w: pictures are limited to %d bytes", ErrTooBig, MaxPictureBytes)
	}
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadPicture, err)
	}
	if cfg.Width > maxPictureSide || cfg.Height > maxPictureSide {
		return "", fmt.Errorf("%w: %dx%d, at most %dx%d", ErrTooBig, cfg.Width, cfg.Height, maxPictureSide, maxPictureSide)
	}
	if _, err := decode(data, a.Size); err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadPicture, err)
	}
	clean, err := cleanName(name, kind)
	if err != nil {
		return "", err
	}
	dir, err := a.channelDir(channel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, clean)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%w: %s; delete it first to replace it", ErrExists, clean)
	}
	return clean, atomicfile.Write(path, data)
}

// First decodes the first frame of one of the channel's own pictures.
func (a Ambient) First(channel, file string) (image.Image, error) {
	if file != filepath.Base(file) || !isPicture(file) {
		return nil, fmt.Errorf("%w: %q", ErrBadName, file)
	}
	dir, err := a.channelDir(channel)
	if err != nil {
		return nil, err
	}
	frames, err := Decode(filepath.Join(dir, file), a.Size)
	if err != nil {
		return nil, err
	}
	return frames[0].Image, nil
}

func (a Ambient) Remove(channel, file string) error {
	if file != filepath.Base(file) || !isPicture(file) {
		return fmt.Errorf("%w: %q", ErrBadName, file)
	}
	dir, err := a.channelDir(channel)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, file)); err != nil {
		return fmt.Errorf("remove picture: %w", err)
	}
	return nil
}

func (a Ambient) RenameChannel(from, to string) error {
	src, err := a.channelDir(from)
	if err != nil {
		return err
	}
	dst, err := a.channelDir(to)
	if err != nil {
		return err
	}
	// Channel names are unique, so a folder already under the new name is a leftover.
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("clear %s: %w", dst, err)
	}
	if err := os.Rename(src, dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("rename pictures of %s: %w", from, err)
	}
	return nil
}

func (a Ambient) DeleteChannel(channel string) error {
	dir, err := a.channelDir(channel)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete pictures of %s: %w", channel, err)
	}
	return nil
}

func (a Ambient) channelDir(channel string) (string, error) {
	if channel == "" || channel == "." || channel == ".." || strings.ContainsAny(channel, `/\`) {
		return "", fmt.Errorf("%w: channel %q", ErrBadName, channel)
	}
	return filepath.Join(a.Dir, channel), nil
}

// cleanName keeps the base name's letters, digits, - and _, and gives it the extension of the
// format the bytes really are.
func cleanName(name, format string) (string, error) {
	base := filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	stem := strings.Trim(unsafeRunes.ReplaceAllString(strings.TrimSuffix(base, filepath.Ext(base)), "-"), "-")
	if len(stem) > 64 {
		stem = stem[:64]
	}
	if stem == "" {
		return "", fmt.Errorf("%w: %q", ErrBadName, name)
	}
	return stem + "." + format, nil
}
