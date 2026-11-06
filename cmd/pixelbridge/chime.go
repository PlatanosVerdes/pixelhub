package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

const chimeRate = 44100

// chimeWAV is two bell notes, G6 then C7, as 16-bit mono PCM: the partials of a small bell fading out.
func chimeWAV() []byte {
	type partial struct{ ratio, gain, decay float64 }
	bell := []partial{{1, 1, 4}, {2.76, 0.45, 7}, {5.4, 0.2, 11}}
	notes := []struct{ at, freq float64 }{{0, 1568}, {0.14, 2093}}
	samples := make([]int16, int(chimeRate*1.1))
	for i := range samples {
		t := float64(i) / chimeRate
		var v float64
		for _, n := range notes {
			if t < n.at {
				continue
			}
			for _, p := range bell {
				v += p.gain * math.Sin(2*math.Pi*n.freq*p.ratio*(t-n.at)) * math.Exp(-(t-n.at)*p.decay)
			}
		}
		v *= 0.35 * min(1, t/0.004)
		samples[i] = int16(max(-1, min(1, v)) * math.MaxInt16)
	}
	return talk.PCM{Rate: chimeRate, Samples: samples}.WAV()
}

// speaker plays sounds on the display's Bluetooth speaker through PipeWire, whose node for a device
// is named after its address.
type speaker struct {
	player string
	log    *slog.Logger
}

// newSpeaker is nil without pw-play.
func newSpeaker(log *slog.Logger) *speaker {
	player, err := exec.LookPath("pw-play")
	if err != nil {
		log.Info("no pw-play, the display stays silent")
		return nil
	}
	return &speaker{player: player, log: log}
}

func (s *speaker) play(ctx context.Context, mac [6]byte, file, what string) {
	node := fmt.Sprintf("bluez_output.%02X_%02X_%02X_%02X_%02X_%02X.1", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, s.player, "--target", node, file).CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			s.log.Info(what+" stopped", "node", node)
			return
		}
		s.log.Warn(what+" failed", "node", node, "err", err, "output", string(bytes.TrimSpace(out)))
		return
	}
	s.log.Info(what+" played", "node", node)
}

// chime is nil without a place to write the sound.
func (s *speaker) chime() func(mac [6]byte) {
	file := filepath.Join(os.TempDir(), "pixelbridge-chime.wav")
	if err := os.WriteFile(file, chimeWAV(), 0o600); err != nil {
		s.log.Warn("chime not written, it stays silent", "err", err)
		return nil
	}
	return func(mac [6]byte) { s.play(context.Background(), mac, file, "chime") }
}

func (s *speaker) say(ctx context.Context, mac [6]byte, wav []byte, after time.Duration) {
	f, err := os.CreateTemp("", "pixelbridge-say-*.wav")
	if err != nil {
		s.log.Warn("sound not written", "err", err)
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(wav)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.log.Warn("sound not written", "err", err)
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(after):
	}
	s.play(ctx, mac, f.Name(), "sound")
}
