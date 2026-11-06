package talk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	// Rate is what every clip is turned into: enough for a voice, small to send.
	Rate = 22050
	// MaxLength is the longest clip kept.
	MaxLength = 30 * time.Second
)

// Effect makes a voice funnier: its pitch and speed scaled, then an ffmpeg filter.
type Effect struct {
	Pitch, Tempo float64
	Filter       string
}

// Effects are the voices Mosko can put on, by name; "" keeps the voice as it is.
var Effects = map[string]Effect{
	"":         {1, 1, ""},
	"squirrel": {1.7, 1.05, ""},
	"cartoon":  {1.35, 1.12, ""},
	"monster":  {0.65, 0.95, ""},
	"robot":    {0.95, 1, "afftfilt=real='hypot(re,im)*sin(0)':imag='hypot(re,im)*cos(0)':win_size=512:overlap=0.75"},
	"nervous":  {1.15, 1.05, "vibrato=f=7:d=0.6"},
	"echo":     {1, 1, "aecho=0.8:0.7:90:0.35"},
}

// Decoder reads any audio ffmpeg knows.
type Decoder struct {
	FFmpeg string
}

// Decode makes audio mono at Rate, cut at MaxLength, its pitch times pitch (1 keeps it) at the same
// speed, with an effect from Effects. The audio goes through a file: phone recordings keep their
// index at the end, out of a pipe's reach.
func (d Decoder) Decode(ctx context.Context, audio []byte, pitch float64, effect string) (PCM, error) {
	if len(audio) == 0 {
		return PCM{}, errors.New("no audio")
	}
	if pitch < 0.5 || pitch > 2 {
		return PCM{}, fmt.Errorf("pitch %.2f is outside 0.5-2", pitch)
	}
	fx, ok := Effects[effect]
	if !ok {
		return PCM{}, fmt.Errorf("no voice effect %q", effect)
	}
	in, err := os.CreateTemp("", "talk-*")
	if err != nil {
		return PCM{}, err
	}
	defer os.Remove(in.Name())
	_, err = in.Write(audio)
	if cerr := in.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return PCM{}, err
	}
	rate := strconv.Itoa(Rate)
	filter := "aresample=" + rate
	if pitch *= fx.Pitch; pitch != 1 || fx.Tempo != 1 {
		filter += fmt.Sprintf(",asetrate=%d,aresample=%s%s", int(Rate*pitch), rate, atempo(fx.Tempo/pitch))
	}
	if fx.Filter != "" {
		filter += "," + fx.Filter
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, d.FFmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-i", in.Name(),
		"-vn", "-t", strconv.Itoa(int(MaxLength.Seconds())), "-af", filter, "-ac", "1", "-ar", rate, "-f", "s16le", "pipe:1")
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return PCM{}, fmt.Errorf("ffmpeg: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	if out.Len() == 0 {
		return PCM{}, errors.New("no sound in that file")
	}
	return fromLE(out.Bytes(), Rate)
}

// atempo chains ffmpeg's atempo, which takes 0.5-2 at a time.
func atempo(x float64) string {
	var out string
	for x < 0.5 {
		out += ",atempo=0.5"
		x /= 0.5
	}
	for x > 2 {
		out += ",atempo=2"
		x /= 2
	}
	return out + fmt.Sprintf(",atempo=%.4f", x)
}
