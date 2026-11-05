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

// ListPicker gives the pictures of a named list in turn.
type ListPicker interface {
	Pick(list string, turn int) (display.Content, bool, error)
}

type ListFunc func(list string, turn int) (display.Content, bool, error)

func (f ListFunc) Pick(list string, turn int) (display.Content, bool, error) { return f(list, turn) }

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
func (p *playlist) at(now time.Time, channel string, rule state.Ambient, stale bool, lists ListPicker) display.Content {
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
	var content display.Content
	var ok bool
	var err error
	if rule.List != "" && lists != nil {
		content, ok, err = lists.Pick(rule.List, p.turn)
	} else {
		content, ok, err = p.source.Pick(channel, p.turn)
	}
	p.turn++
	if err != nil {
		p.log.Warn("ambient picture skipped", "channel", channel, "err", err)
	}
	if ok {
		p.show = content
	}
	return p.show
}
