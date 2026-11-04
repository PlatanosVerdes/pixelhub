package divoom

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math/bits"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	Size      = 16
	MaxFrames = 60

	frameStart = 0x01
	frameEnd   = 0x02

	cmdResponse   = 0x04
	cmdImage      = 0x44
	cmdView       = 0x45
	cmdAnimation  = 0x49
	cmdBrightness = 0x74

	respOK       = 0x55
	respGreeting = 0xbd

	chunkSize    = 200
	maxFrame     = 4096
	maxAnimation = 256 * chunkSize
	pictureMark  = 0xaa
)

func Message(cmd byte, payload []byte) []byte {
	out := make([]byte, 0, len(payload)+6)
	out = binary.LittleEndian.AppendUint16(append(out, frameStart), uint16(len(payload)+3))
	out = append(out, cmd)
	out = append(out, payload...)
	var sum uint16
	for _, b := range out[1:] {
		sum += uint16(b)
	}
	out = binary.LittleEndian.AppendUint16(out, sum)
	return append(out, frameEnd)
}

func Brightness(level int) []byte {
	return Message(cmdBrightness, []byte{byte(clamp(level))})
}

func Light(c color.RGBA, level int) []byte {
	return Message(cmdView, []byte{0x01, c.R, c.G, c.B, byte(clamp(level)), 0x00, 0x01, 0x00, 0x00, 0x00})
}

func Clock() []byte {
	return Message(cmdView, []byte{0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
}

func Image(img image.Image) []byte {
	return Message(cmdImage, append([]byte{0x00, 0x0a, 0x0a, 0x04}, Frame(img, 0)...))
}

// Encode returns the messages that paint c, in order; the Evo needs a short pause between them.
func Encode(c display.Content) ([][]byte, error) {
	brightness := Brightness(c.Brightness)
	switch {
	case c.Kind == display.Clock:
		return [][]byte{Clock(), brightness}, nil
	case c.Kind == display.Color:
		return [][]byte{Light(c.Fill, c.Brightness), brightness}, nil
	case len(c.Frames) == 0:
		return nil, errors.New("picture without frames")
	case len(c.Frames) == 1:
		return [][]byte{Image(c.Frames[0].Image), brightness}, nil
	}
	return append(Animation(c.Frames), brightness), nil
}

// Animation keeps the first frames that fit: at most MaxFrames, and no more chunks than the one-byte
// chunk index can number.
func Animation(frames []display.Frame) [][]byte {
	var all []byte
	for _, f := range frames[:min(len(frames), MaxFrames)] {
		frame := Frame(f.Image, f.Delay)
		if len(all)+len(frame) > maxAnimation {
			break
		}
		all = append(all, frame...)
	}
	var msgs [][]byte
	for i := 0; i < len(all); i += chunkSize {
		chunk := all[i:min(i+chunkSize, len(all))]
		payload := binary.LittleEndian.AppendUint16(nil, uint16(len(all)))
		payload = append(payload, byte(i/chunkSize))
		msgs = append(msgs, Message(cmdAnimation, append(payload, chunk...)))
	}
	return msgs
}

func Frame(img image.Image, delay time.Duration) []byte {
	palette, pixels := indexed(img)
	body := binary.LittleEndian.AppendUint16(nil, uint16(delay.Milliseconds()))
	body = append(body, 0x00, byte(len(palette)))
	for _, c := range palette {
		body = append(body, c.R, c.G, c.B)
	}
	body = append(body, pack(pixels, bitsPerPixel(len(palette)))...)
	out := binary.LittleEndian.AppendUint16([]byte{pictureMark}, uint16(len(body)+3))
	return append(out, body...)
}

func indexed(img image.Image) ([]color.RGBA, []int) {
	var palette []color.RGBA
	index := map[color.RGBA]int{}
	b := img.Bounds()
	pixels := make([]int, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := opaque(img.At(x, y))
			i, ok := index[c]
			if !ok {
				i = len(palette)
				index[c] = i
				palette = append(palette, c)
			}
			pixels = append(pixels, i)
		}
	}
	return palette, pixels
}

func opaque(c color.Color) color.RGBA {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	if n.A <= 32 {
		return color.RGBA{A: 0xff}
	}
	return color.RGBA{R: n.R, G: n.G, B: n.B, A: 0xff}
}

func bitsPerPixel(colors int) int {
	return max(1, bits.Len(uint(colors-1)))
}

func pack(pixels []int, width int) []byte {
	var out []byte
	acc, n := 0, 0
	for _, p := range pixels {
		acc |= p << n
		n += width
		for n >= 8 {
			out = append(out, byte(acc))
			acc >>= 8
			n -= 8
		}
	}
	if n > 0 {
		out = append(out, byte(acc))
	}
	return out
}

func clamp(level int) int { return min(max(level, 0), 100) }

// Ack returns the command an Evo response frame acknowledges.
func Ack(frame []byte) (byte, bool) {
	if len(frame) < 7 || frame[3] != cmdResponse || frame[5] != respOK {
		return 0, false
	}
	return frame[4], true
}

func IsGreeting(frame []byte) bool {
	cmd, ok := Ack(frame)
	return ok && cmd == respGreeting
}

// NextFrame finds the frame at the start of buf. n is 0 when more bytes are needed; with a nil
// frame, n bytes are not a frame and should be skipped.
func NextFrame(buf []byte) (frame []byte, n int) {
	if len(buf) == 0 {
		return nil, 0
	}
	if buf[0] != frameStart {
		return nil, 1
	}
	size := FrameLen(buf)
	switch {
	case size == 0:
		return nil, 0
	case size > maxFrame:
		return nil, 1
	case len(buf) < size:
		return nil, 0
	case buf[size-1] != frameEnd:
		return nil, 1
	}
	return buf[:size], size
}

// FrameLen returns the full length of the frame starting at buf[0], or 0 when the header is incomplete.
func FrameLen(buf []byte) int {
	if len(buf) < 3 {
		return 0
	}
	return int(binary.LittleEndian.Uint16(buf[1:3])) + 4
}
