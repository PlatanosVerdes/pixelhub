package sprite

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const defaultGIFDelay = 100 * time.Millisecond

type Library struct {
	sprites map[string][]display.Frame
}

// Load returns the built-in sprites plus every GIF or PNG in dir, which win by name. A missing dir is fine.
func Load(dir string, size int) (*Library, error) {
	lib := &Library{sprites: map[string][]display.Frame{}}
	for name, a := range builtin {
		frames, err := a.render()
		if err != nil {
			return nil, fmt.Errorf("built-in sprite %s: %w", name, err)
		}
		for i := range frames {
			frames[i].Image = Resize(frames[i].Image, size)
		}
		lib.sprites[name] = frames
	}
	files, err := pictureFiles(dir)
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		frames, err := Decode(path, size)
		if err != nil {
			return nil, err
		}
		lib.sprites[nameOf(path)] = frames
	}
	return lib, nil
}

func (l *Library) Get(name string) (display.Content, bool) {
	frames, ok := l.sprites[name]
	return display.Content{Kind: display.Picture, Name: name, Frames: frames}, ok
}

func (l *Library) Names() []string {
	return slices.Sorted(maps.Keys(l.sprites))
}

func pictureFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && isPicture(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

func nameOf(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func Decode(path string, size int) ([]display.Frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open sprite: %w", err)
	}
	frames, err := decode(data, size)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return frames, nil
}

// DecodeBytes reads a GIF or PNG held in memory.
func DecodeBytes(data []byte, size int) ([]display.Frame, error) { return decode(data, size) }

// decode trusts the bytes, not the file name, to tell GIF from PNG.
func decode(data []byte, size int) ([]display.Frame, error) {
	var frames []display.Frame
	switch format(data) {
	case "gif":
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		frames = gifFrames(g)
	case "png":
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		frames = []display.Frame{{Image: img}}
	default:
		return nil, errors.New("not a GIF or PNG")
	}
	if len(frames) == 0 {
		return nil, errors.New("no frames")
	}
	for i := range frames {
		frames[i].Image = Resize(frames[i].Image, size)
	}
	if len(frames) == 1 {
		frames[0].Delay = 0
	}
	return frames, nil
}

var pictureFormats = []string{"gif", "png"}

// isPicture tells a picture file by its extension; dot files are the atomic writer's temporaries.
func isPicture(name string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	return !strings.HasPrefix(name, ".") && slices.Contains(pictureFormats, ext)
}

func format(data []byte) string {
	_, name, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	return name
}

// gifFrames composites each GIF frame over the previous ones, since GIF frames are usually partial.
func gifFrames(g *gif.GIF) []display.Frame {
	bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	if bounds.Empty() && len(g.Image) > 0 {
		bounds = g.Image[0].Bounds()
	}
	canvas := image.NewNRGBA(bounds)
	frames := make([]display.Frame, 0, len(g.Image))
	for i, img := range g.Image {
		var previous *image.NRGBA
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			previous = clone(canvas)
		}
		draw.Draw(canvas, img.Bounds(), img, img.Bounds().Min, draw.Over)
		delay := defaultGIFDelay
		if i < len(g.Delay) && g.Delay[i] > 0 {
			delay = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		frames = append(frames, display.Frame{Image: clone(canvas), Delay: delay})
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, img.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = previous
		}
	}
	return frames
}

func clone(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// Resize scales src to size x size, nearest neighbour.
func Resize(src image.Image, size int) image.Image {
	b := src.Bounds()
	if b.Dx() == size && b.Dy() == size {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			dst.Set(x, y, src.At(b.Min.X+x*b.Dx()/size, b.Min.Y+y*b.Dy()/size))
		}
	}
	return dst
}

func (a art) render() ([]display.Frame, error) {
	const size = 16
	frames := make([]display.Frame, 0, len(a.frames))
	for _, rows := range a.frames {
		if len(rows) != size {
			return nil, fmt.Errorf("%d rows, want %d", len(rows), size)
		}
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		for y, row := range rows {
			if len(row) != size {
				return nil, fmt.Errorf("row %d is %d wide, want %d", y, len(row), size)
			}
			for x, ch := range row {
				c, ok := color.RGBA{A: 255}, true
				if ch != '.' {
					c, ok = a.palette[ch]
				}
				if !ok {
					return nil, fmt.Errorf("row %d: %q is not in the palette", y, ch)
				}
				img.Set(x, y, c)
			}
		}
		frames = append(frames, display.Frame{Image: img, Delay: a.delay})
	}
	return frames, nil
}

// ClockFace stands in for the display's own clock in previews.
var ClockFace = sync.OnceValue(func() image.Image {
	frames, err := clockFace.render()
	if err != nil {
		panic(err)
	}
	return frames[0].Image
})

// Render draws what c would show, at size x size.
func Render(c display.Content, size int) image.Image {
	switch {
	case c.Kind == display.Picture && len(c.Frames) > 0:
		return c.Frames[0].Image
	case c.Kind == display.Native:
		return renderView(c.View, size)
	case c.Kind == display.Color:
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.Draw(img, img.Bounds(), image.NewUniform(c.Fill), image.Point{}, draw.Src)
		return img
	default:
		return Resize(ClockFace(), size)
	}
}
