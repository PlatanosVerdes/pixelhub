package bridge

import (
	"slices"

	"github.com/PlatanosVerdes/pixelhub/internal/divoom"
)

type event struct {
	frame   []byte
	lost    bool
	battery int // 1-10 when the bridge reported a battery level of battery-1
	light   int // 1-256 when the bridge's light sensor read light-1
	sound   bool
	speaker int // 1 when the display's speaker was not there for a talk, 2 when it played it
	err     error
}

const (
	// opBattery is followed by the display's battery level, 0-9, read by the bridge over hands-free.
	opBattery = 0x42
	// opLight is followed by the light the bridge's sensor sees, 0 dark to 255; opSound by a byte the
	// sound sensor sets, which only says a sound came.
	opLight = 0x4c
	opSound = 0x53
	// opSpeaker is followed by 1 when the display's speaker played a talk, 0 when it was not connected.
	opSpeaker = 0x41
)

// parser splits the bridge's byte stream into Evo frames and link-down bytes, whatever the read boundaries.
type parser struct {
	buf []byte
}

func (p *parser) feed(data []byte) []event {
	p.buf = append(p.buf, data...)
	var out []event
	for len(p.buf) > 0 {
		if p.buf[0] == opLight || p.buf[0] == opSound {
			if len(p.buf) < 2 {
				break
			}
			if p.buf[0] == opLight {
				out = append(out, event{light: int(p.buf[1]) + 1})
			} else {
				out = append(out, event{sound: true})
			}
			p.buf = p.buf[2:]
			continue
		}
		if p.buf[0] == opSpeaker {
			if len(p.buf) < 2 {
				break
			}
			out = append(out, event{speaker: 1 + int(min(p.buf[1], 1))})
			p.buf = p.buf[2:]
			continue
		}
		if p.buf[0] == opBattery {
			if len(p.buf) < 2 {
				break
			}
			if p.buf[1] <= 9 {
				out = append(out, event{battery: int(p.buf[1]) + 1})
			}
			p.buf = p.buf[2:]
			continue
		}
		if p.buf[0] == opLinkDown {
			out = append(out, event{lost: true})
			p.buf = p.buf[1:]
			continue
		}
		frame, n := divoom.NextFrame(p.buf)
		if n == 0 {
			break
		}
		if frame != nil {
			out = append(out, event{frame: slices.Clone(frame)})
		}
		p.buf = p.buf[n:]
	}
	return out
}
