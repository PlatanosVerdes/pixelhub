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
	// MaxLength is the longest clip kept; the display holds at most 60 frames of mouth.
	MaxLength = 30 * time.Second
)

// Decoder reads any audio ffmpeg knows.
type Decoder struct {
	FFmpeg string
}

// Decode makes audio mono at Rate, cut at MaxLength, its pitch times pitch (1 keeps it) at the same
// speed. The audio goes through a file: phone recordings keep their index at the end, out of a pipe's reach.
func (d Decoder) Decode(ctx context.Context, audio []byte, pitch float64) (PCM, error) {
	if len(audio) == 0 {
		return PCM{}, errors.New("no audio")
	}
	if pitch < 0.5 || pitch > 2 {
		return PCM{}, fmt.Errorf("pitch %.2f is outside 0.5-2", pitch)
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
	if pitch != 1 {
		filter += fmt.Sprintf(",asetrate=%d,aresample=%s,atempo=%.4f", int(Rate*pitch), rate, 1/pitch)
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
