package divoom

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

var referencePalette = map[rune]color.Color{
	'.': color.RGBA{0, 0, 0, 255},
	'R': color.RGBA{230, 0, 40, 255},
	'W': color.RGBA{255, 255, 255, 255},
}

func art(t *testing.T, name string) image.Image {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Fields(string(data))
	img := image.NewNRGBA(image.Rect(0, 0, len(rows[0]), len(rows)))
	for y, row := range rows {
		for x, ch := range row {
			img.Set(x, y, referencePalette[ch])
		}
	}
	return img
}

func golden(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".hex"))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for line := range strings.FieldsSeq(string(data)) {
		b, err := hex.DecodeString(line)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func TestMatchesPythonReference(t *testing.T) {
	t.Parallel()
	heart, small := art(t, "heart"), art(t, "small")
	const beat = 400 * time.Millisecond
	tests := []struct {
		name   string
		golden string
		build  func() ([][]byte, error)
	}{
		{"still heart", "heart_image", func() ([][]byte, error) { return [][]byte{Image(heart)}, nil }},
		{"two frame heartbeat", "heartbeat_animation", func() ([][]byte, error) {
			return Animation([]display.Frame{{Image: heart, Delay: beat}, {Image: small, Delay: beat}}), nil
		}},
		{"four frames in two chunks", "heartbeat_twice_animation", func() ([][]byte, error) {
			return Animation([]display.Frame{
				{Image: heart, Delay: beat}, {Image: small, Delay: beat},
				{Image: heart, Delay: 250 * time.Millisecond}, {Image: small, Delay: 250 * time.Millisecond},
			}), nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.build()
			if err != nil {
				t.Fatal(err)
			}
			want := golden(t, tt.golden)
			if len(got) != len(want) {
				t.Fatalf("%d messages, want %d", len(got), len(want))
			}
			for i := range want {
				if !bytes.Equal(got[i], want[i]) {
					t.Errorf("message %d\n got %x\nwant %x", i, got[i], want[i])
				}
			}
		})
	}
}

func TestMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  []byte
		want string
	}{
		{"brightness", Brightness(50), "0104007432aa0002"},
		{"brightness is clamped", Brightness(150), "0104007464dc0002"},
		{"light", Light(color.RGBA{R: 255, G: 128, B: 1}, 100), "010d004501ff8001640001000000380202"},
		{"clock", Clock(), "010e00450001000100000000000000550002"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hex.EncodeToString(tt.got); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPaletteFollowsFirstSeenOrder(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	blue, green, faint := color.NRGBA{0, 0, 255, 255}, color.NRGBA{0, 255, 0, 255}, color.NRGBA{200, 0, 0, 32}
	for i, c := range []color.Color{blue, green, blue, faint, green, blue} {
		img.Set(i%3, i/3, c)
	}
	palette, pixels := indexed(img)
	wantPalette := []color.RGBA{{0, 0, 255, 255}, {0, 255, 0, 255}, {0, 0, 0, 255}}
	if !slices.Equal(palette, wantPalette) {
		t.Errorf("palette %v, want %v", palette, wantPalette)
	}
	if want := []int{0, 1, 0, 2, 1, 0}; !slices.Equal(pixels, want) {
		t.Errorf("pixels %v, want %v", pixels, want)
	}
}

func TestBitsPerPixel(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ colors, bits int }{
		{1, 1}, {2, 1}, {3, 2}, {4, 2}, {5, 3}, {8, 3}, {9, 4}, {129, 8}, {256, 8},
	} {
		if got := bitsPerPixel(tt.colors); got != tt.bits {
			t.Errorf("bitsPerPixel(%d) = %d, want %d", tt.colors, got, tt.bits)
		}
	}
}

func TestPack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		pixels []int
		width  int
		want   []byte
	}{
		{"1 bit", []int{1, 0, 1, 1, 0, 0, 0, 1, 1}, 1, []byte{0b10001101, 0b1}},
		{"2 bits", []int{1, 2, 3, 0, 2}, 2, []byte{0b00111001, 0b10}},
		{"3 bits", []int{5, 7, 2, 1}, 3, []byte{0b10111101, 0b0010}},
		{"8 bits", []int{0, 255, 17}, 8, []byte{0, 255, 17}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pack(tt.pixels, tt.width); !bytes.Equal(got, tt.want) {
				t.Errorf("got %08b, want %08b", got, tt.want)
			}
		})
	}
}

func TestFullPaletteWritesZeroColors(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, Size, Size))
	for i := range Size * Size {
		img.Set(i%Size, i/Size, color.NRGBA{uint8(i), uint8(255 - i), 7, 255})
	}
	f := Frame(img, 0)
	if f[6] != 0 {
		t.Errorf("colour count byte %d, want 0 for 256 colours", f[6])
	}
	if want := 3 + 4 + 256*3 + 256; len(f) != want {
		t.Errorf("frame is %d bytes, want %d", len(f), want)
	}
	if got := int(f[1]) | int(f[2])<<8; got != len(f) {
		t.Errorf("frame length field %d, want %d", got, len(f))
	}
}

func TestAnimationKeepsTheFirstFrames(t *testing.T) {
	t.Parallel()
	frames := make([]display.Frame, MaxFrames+5)
	for i := range frames {
		frames[i].Image = image.NewNRGBA(image.Rect(0, 0, Size, Size))
	}
	msgs := Animation(frames)
	if total := int(msgs[0][4]) | int(msgs[0][5])<<8; total != MaxFrames*len(Frame(frames[0].Image, 0)) {
		t.Errorf("animation is %d bytes, want %d frames", total, MaxFrames)
	}
}

func TestAnimationFitsTheChunkIndex(t *testing.T) {
	t.Parallel()
	frames := make([]display.Frame, MaxFrames)
	for n := range frames {
		img := image.NewNRGBA(image.Rect(0, 0, Size, Size))
		for i := range Size * Size {
			img.Set(i%Size, i/Size, color.NRGBA{uint8(i), uint8(n), 7, 255})
		}
		frames[n].Image = img
	}
	msgs := Animation(frames)
	if len(msgs) > 256 {
		t.Errorf("%d chunks, the index only counts 256", len(msgs))
	}
	if total := int(msgs[0][4]) | int(msgs[0][5])<<8; total > maxAnimation || total%len(Frame(frames[0].Image, 0)) != 0 {
		t.Errorf("animation is %d bytes, want whole frames up to %d", total, maxAnimation)
	}
}

func TestEncode(t *testing.T) {
	t.Parallel()
	still := display.Frame{Image: image.NewNRGBA(image.Rect(0, 0, Size, Size))}
	tests := []struct {
		name     string
		content  display.Content
		commands []byte
	}{
		{"clock", display.ClockContent(), []byte{cmdView, cmdBrightness}},
		{"color", display.ColorContent(color.RGBA{R: 9}), []byte{cmdView, cmdBrightness}},
		{"still", display.Content{Kind: display.Picture, Frames: []display.Frame{still}}, []byte{cmdImage, cmdBrightness}},
		{"animation", display.Content{Kind: display.Picture, Frames: []display.Frame{still, still}}, []byte{cmdAnimation, cmdBrightness}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msgs, err := Encode(tt.content.WithBrightness(30))
			if err != nil {
				t.Fatal(err)
			}
			var got []byte
			for _, m := range msgs {
				got = append(got, m[3])
			}
			if !bytes.Equal(got, tt.commands) {
				t.Errorf("commands %x, want %x", got, tt.commands)
			}
		})
	}
	if _, err := Encode(display.Content{Kind: display.Picture}); err == nil {
		t.Error("want an error for a picture without frames")
	}
}

func TestAck(t *testing.T) {
	t.Parallel()
	greeting, _ := hex.DecodeString("01090004bd5513011e00510102")
	tests := []struct {
		name     string
		frame    []byte
		cmd      byte
		ok       bool
		greeting bool
	}{
		{"greeting", greeting, 0xbd, true, true},
		{"brightness ack", Message(0x04, []byte{0x32, 0x55, 0x00}), 0x32, true, false},
		{"not a response", Brightness(10), 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, ok := Ack(tt.frame)
			if cmd != tt.cmd || ok != tt.ok || IsGreeting(tt.frame) != tt.greeting {
				t.Errorf("Ack = %x %v, greeting %v", cmd, ok, IsGreeting(tt.frame))
			}
		})
	}
}
