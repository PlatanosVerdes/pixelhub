package hub

import (
	"slices"
	"time"
)

// queue holds alerts by priority, oldest first within one; items[0] has been on screen since `since`.
// A higher priority preempts the one showing, which keeps its remaining time.
type queue struct {
	items []Alert
	since time.Time
}

func (q *queue) push(a Alert, now time.Time) {
	q.advance(now)
	at := len(q.items)
	for i, it := range q.items {
		if a.Priority > it.Priority {
			at = i
			break
		}
	}
	if at == 0 {
		if len(q.items) > 0 {
			q.items[0].Duration -= now.Sub(q.since)
		}
		q.since = now
	}
	q.items = slices.Insert(q.items, at, a)
}

// drop removes every alert from source; the next one starts its time now if the head went.
func (q *queue) drop(source string, now time.Time) {
	q.advance(now)
	headGone := len(q.items) > 0 && q.items[0].Source == source
	q.items = slices.DeleteFunc(q.items, func(a Alert) bool { return a.Source == source })
	if headGone {
		q.since = now
	}
}

func (q *queue) head(now time.Time) (Alert, bool) {
	q.advance(now)
	if len(q.items) == 0 {
		return Alert{}, false
	}
	return q.items[0], true
}

func (q *queue) advance(now time.Time) {
	for len(q.items) > 0 {
		end := q.since.Add(q.items[0].Duration)
		if now.Before(end) {
			return
		}
		q.items = q.items[1:]
		q.since = end
	}
}
