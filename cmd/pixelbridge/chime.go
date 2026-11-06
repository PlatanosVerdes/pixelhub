package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
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
	var b bytes.Buffer
	data := len(samples) * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, []any{uint32(16), uint16(1), uint16(1), uint32(chimeRate), uint32(chimeRate * 2), uint16(2), uint16(16)})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(data))
	_ = binary.Write(&b, binary.LittleEndian, samples)
	return b.Bytes()
}

// chimePlayer plays the chime on the display's Bluetooth speaker through PipeWire, whose node for a
// device is named after its address. Without pw-play, or without a place to write the sound, it is nil.
func chimePlayer(log *slog.Logger) func(mac [6]byte) {
	player, err := exec.LookPath("pw-play")
	if err != nil {
		log.Info("no pw-play, the chime stays silent")
		return nil
	}
	file := filepath.Join(os.TempDir(), "pixelbridge-chime.wav")
	if err := os.WriteFile(file, chimeWAV(), 0o600); err != nil {
		log.Warn("chime not written, it stays silent", "err", err)
		return nil
	}
	return func(mac [6]byte) {
		node := fmt.Sprintf("bluez_output.%02X_%02X_%02X_%02X_%02X_%02X.1", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, player, "--target", node, file).CombinedOutput(); err != nil {
			log.Warn("chime failed", "node", node, "err", err, "output", string(bytes.TrimSpace(out)))
			return
		}
		log.Info("chime played", "node", node)
	}
}
