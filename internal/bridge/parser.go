package bridge

import (
	"slices"

	"github.com/PlatanosVerdes/pixelhub/internal/divoom"
)

type event struct {
	frame []byte
	lost  bool
	err   error
}

// parser splits the bridge's byte stream into Evo frames and link-down bytes, whatever the read boundaries.
type parser struct {
	buf []byte
}

func (p *parser) feed(data []byte) []event {
	p.buf = append(p.buf, data...)
	var out []event
	for len(p.buf) > 0 {
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
