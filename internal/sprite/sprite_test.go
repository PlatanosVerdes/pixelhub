package sprite

import (
	"bytes"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/divoom"
)

func TestBuiltins(t *testing.T) {
	t.Parallel()
	lib, err := Load(t.TempDir(), divoom.Size)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alert", "battery-full", "battery-low", "check", "coffee", "heart", "heartbeat", "lunch", "moon", "sun", "sunrise", "zzz"}
	if got := lib.Names(); !slices.Equal(got, want) {
		t.Errorf("names %v, want %v", got, want)
	}
	frames := map[string]int{"heartbeat": 2, "zzz": 3, "moon": 1, "battery-low": 2}
	for name, n := range frames {
		c, _ := lib.Get(name)
		if len(c.Frames) != n {
			t.Errorf("%s has %d frames, want %d", name, len(c.Frames), n)
		}
	}
}

func TestBuiltinHeartIsTheTestedOne(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../divoom/testdata/heart_image.hex")
	if err != nil {
		t.Fatal(err)
	}
	lib, err := Load("", divoom.Size)
	if err != nil {
		t.Fatal(err)
	}
	heart, _ := lib.Get("heart")
	if got := hex.EncodeToString(divoom.Image(heart.Frames[0].Image)); got != strings.TrimSpace(string(data)) {
		t.Errorf("heart encodes to %s", got)
	}
}

func TestFilesOverrideAndExtend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "heart.png"), 32, color.NRGBA{0, 0, 255, 255})
	writePNG(t, filepath.Join(dir, "Plant.PNG"), 16, color.NRGBA{0, 255, 0, 255})
	lib, err := Load(dir, divoom.Size)
	if err != nil {
		t.Fatal(err)
	}
	heart, _ := lib.Get("heart")
	if b := heart.Frames[0].Image.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("heart resized to %v", b)
	}
	if r, g, b, _ := heart.Frames[0].Image.At(3, 3).RGBA(); r != 0 || g != 0 || b != 0xffff {
		t.Errorf("heart not overridden: %x %x %x", r, g, b)
	}
	if _, ok := lib.Get("Plant"); !ok {
		t.Error("Plant.PNG not loaded")
	}
}

func TestGIFFramesComposite(t *testing.T) {
	t.Parallel()
	pal := color.Palette{color.Transparent, color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}}
	full := image.NewPaletted(image.Rect(0, 0, 8, 8), pal)
	for i := range full.Pix {
		full.Pix[i] = 1
	}
	patch := image.NewPaletted(image.Rect(2, 2, 4, 4), pal)
	for i := range patch.Pix {
		patch.Pix[i] = 2
	}
	path := filepath.Join(t.TempDir(), "blink.gif")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = gif.EncodeAll(f, &gif.GIF{
		Image:    []*image.Paletted{full, patch},
		Delay:    []int{20, 0},
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config:   image.Config{ColorModel: pal, Width: 8, Height: 8},
	})
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	frames, err := Decode(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].Delay != 200*time.Millisecond || frames[1].Delay != defaultGIFDelay {
		t.Fatalf("frames %d, delays %v %v", len(frames), frames[0].Delay, frames[1].Delay)
	}
	tests := []struct {
		x, y int
		want color.NRGBA
	}{
		{0, 0, color.NRGBA{255, 0, 0, 255}},
		{4, 4, color.NRGBA{0, 0, 255, 255}},
		{7, 7, color.NRGBA{0, 0, 255, 255}},
		{8, 8, color.NRGBA{255, 0, 0, 255}},
	}
	for _, tt := range tests {
		if got := color.NRGBAModel.Convert(frames[1].Image.At(tt.x, tt.y)); got != tt.want {
			t.Errorf("second frame at %d,%d = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
}

func TestAmbientPick(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := Ambient{Dir: dir, Size: divoom.Size}
	if _, ok, err := a.Pick("office", 0); ok || err != nil {
		t.Fatalf("empty dir picked something: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "office"), 0o700); err != nil {
		t.Fatal(err)
	}
	red := color.NRGBA{255, 0, 0, 255}
	writePNG(t, filepath.Join(dir, "a.png"), 16, red)
	writePNG(t, filepath.Join(dir, "b.png"), 16, red)
	writePNG(t, filepath.Join(dir, "office", "w.png"), 16, red)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		channel string
		turn    int
		want    string
	}{
		{"evening", 0, "a.png"},
		{"evening", 1, "b.png"},
		{"evening", 2, "a.png"},
		{"office", 0, "office/w.png"},
		{"office", 1, "office/w.png"},
	}
	for _, tt := range tests {
		c, ok, err := a.Pick(tt.channel, tt.turn)
		if !ok || err != nil || c.Name != tt.want {
			t.Errorf("%s turn %d picked %q (%v), want %s", tt.channel, tt.turn, c.Name, err, tt.want)
		}
	}
}

func TestAmbientAdd(t *testing.T) {
	t.Parallel()
	pngData := encodePNG(t, 16)
	gifData := encodeGIF(t)
	tests := []struct {
		name     string
		filename string
		data     []byte
		saved    string
		err      error
	}{
		{"png", "cat.png", pngData, "cat.png", nil},
		{"gif", "loop.gif", gifData, "loop.gif", nil},
		{"extension follows the bytes", "cat.gif", pngData, "cat.png", nil},
		{"odd characters", "my cat (1).png", pngData, "my-cat-1.png", nil},
		{"path traversal", "../../etc/passwd.png", pngData, "passwd.png", nil},
		{"windows traversal", `..\..\boot.png`, pngData, "boot.png", nil},
		{"nothing left of the name", "../.png", pngData, "", ErrBadName},
		{"not an image", "notes.png", []byte("hello"), "", ErrBadPicture},
		{"jpeg", "photo.png", []byte("\xff\xd8\xff\xe0rest"), "", ErrBadPicture},
		{"truncated png", "cut.png", pngData[:len(pngData)/2], "", ErrBadPicture},
		{"huge dimensions", "huge.png", encodePNG(t, 600), "", ErrTooBig},
		{"too big", "big.png", append(append([]byte{}, pngData...), make([]byte, MaxPictureBytes)...), "", ErrTooBig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			a := Ambient{Dir: dir, Size: divoom.Size}
			saved, err := a.Add("office", tt.filename, tt.data)
			if !errors.Is(err, tt.err) || saved != tt.saved {
				t.Fatalf("Add = %q, %v; want %q, %v", saved, err, tt.saved, tt.err)
			}
			files, _ := a.List("office")
			want := []string{}
			if tt.saved != "" {
				want = []string{tt.saved}
			}
			if !slices.Equal(files, want) {
				t.Errorf("office holds %v, want %v", files, want)
			}
		})
	}
}

func TestAmbientChannelFolders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := Ambient{Dir: dir, Size: divoom.Size}
	if _, err := a.Add("office", "cat.png", encodePNG(t, 16)); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove("office", "../cat.png"); !errors.Is(err, ErrBadName) {
		t.Errorf("remove with a path: %v", err)
	}
	if err := a.Remove("office", "dog.png"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("remove a missing file: %v", err)
	}
	if err := a.RenameChannel("office", "studio"); err != nil {
		t.Fatal(err)
	}
	if files, _ := a.List("studio"); !slices.Equal(files, []string{"cat.png"}) {
		t.Errorf("studio holds %v", files)
	}
	if _, err := a.Add("studio", "cat.png", encodePNG(t, 16)); !errors.Is(err, ErrExists) {
		t.Errorf("second upload under the same name: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "leftover"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leftover", "old.png"), encodePNG(t, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.RenameChannel("studio", "leftover"); err != nil {
		t.Fatalf("rename onto a leftover folder: %v", err)
	}
	if files, _ := a.List("leftover"); !slices.Equal(files, []string{"cat.png"}) {
		t.Errorf("leftover holds %v, want only the renamed channel's pictures", files)
	}
	if err := a.RenameChannel("leftover", "studio"); err != nil {
		t.Fatal(err)
	}
	if err := a.RenameChannel("empty", "other"); err != nil {
		t.Errorf("renaming a channel without pictures: %v", err)
	}
	if err := a.Remove("studio", "cat.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Add("studio", "cat.png", encodePNG(t, 16)); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteChannel("studio"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "studio")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("studio folder still there: %v", err)
	}
	if _, err := a.List(".."); !errors.Is(err, ErrBadName) {
		t.Errorf("list outside the folder: %v", err)
	}
}

func TestClockFace(t *testing.T) {
	t.Parallel()
	if b := ClockFace().Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("clock face is %v", b)
	}
}

func encodePNG(t *testing.T, size int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, size, size))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeGIF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	pal := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 16, 16), pal)
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writePNG(t *testing.T, path string, size int, c color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
