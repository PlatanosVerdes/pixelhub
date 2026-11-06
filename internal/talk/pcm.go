// Package talk turns speech into a face whose mouth follows it on the display: the audio comes from
// a recording or from text read by a speech server, and the mouth opens as loud as the voice is.
package talk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
)

// PCM is mono 16-bit audio.
type PCM struct {
	Rate    int
	Samples []int16
}

func (p PCM) Duration() time.Duration {
	if p.Rate <= 0 {
		return 0
	}
	return time.Duration(len(p.Samples)) * time.Second / time.Duration(p.Rate)
}

// WAV wraps the samples in a RIFF header, as any player takes them.
func (p PCM) WAV() []byte {
	var b bytes.Buffer
	data := len(p.Samples) * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, []any{uint32(16), uint16(1), uint16(1), uint32(p.Rate), uint32(p.Rate * 2), uint16(2), uint16(16)})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(data))
	_ = binary.Write(&b, binary.LittleEndian, p.Samples)
	return b.Bytes()
}

func fromLE(raw []byte, rate int) (PCM, error) {
	if len(raw)%2 != 0 {
		return PCM{}, errors.New("odd number of bytes in 16-bit audio")
	}
	samples := make([]int16, len(raw)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	return PCM{Rate: rate, Samples: samples}, nil
}
