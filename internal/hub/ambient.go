package hub

import (
	"log/slog"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
)

type picker interface {
	Pick(channel string, turn int) (display.Content, bool, error)
}

// playlist alternates a stretch of pictures with a stretch of the native clock. Only update uses it,
// so it needs no lock.
type playlist struct {
	source  picker
	log     *slog.Logger
	channel string
	rule    state.Ambient
	clock   bool
	end     time.Time
	turn    int
	show    display.Content
}

// at restarts the playlist when the channel, its rule or its pictures (stale) have changed.
func (p *playlist) at(now time.Time, channel string, rule state.Ambient, stale bool) display.Content {
	if stale || channel != p.channel || rule != p.rule {
		p.channel, p.rule, p.clock, p.end = channel, rule, true, time.Time{}
	}
	if now.Before(p.end) {
		return p.show
	}
	p.clock = !p.clock
	if rule.Clock == 0 {
		p.clock = false
	}
	if rule.Pictures == 0 {
		p.clock = true
	}
	p.show = display.ClockContent()
	if p.clock {
		p.end = now.Add(time.Duration(rule.Clock))
		return p.show
	}
	p.end = now.Add(time.Duration(rule.Pictures))
	content, ok, err := p.source.Pick(channel, p.turn)
	p.turn++
	if err != nil {
		p.log.Warn("ambient picture skipped", "channel", channel, "err", err)
	}
	if ok {
		p.show = content
	}
	return p.show
}
