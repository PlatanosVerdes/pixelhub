// Package calendar follows a calendar through its secret iCal address and warns before each event.
package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
)

const (
	DefaultLead = 10 * time.Minute
	MaxLead     = 60 * time.Minute
	fetchEvery  = 5 * time.Minute
	checkEvery  = 20 * time.Second
	lookAhead   = 7 * 24 * time.Hour
	maxFeedSize = 8 << 20
	shownNext   = 5
)

var ErrNotConfigured = errors.New("no calendar address yet")

type Status struct {
	Configured  bool      `json:"configured"`
	Account     string    `json:"account,omitempty"`
	LeadMinutes int       `json:"lead_minutes"`
	Next        []Event   `json:"next"`
	FetchedAt   time.Time `json:"fetched_at,omitzero"`
	Error       string    `json:"error,omitempty"`
}

type saved struct {
	URL         string `json:"url"`
	LeadMinutes int    `json:"lead_minutes,omitempty"`
}

// Warn is called once per event, when it is lead away or less, with the time left.
type Warn func(e Event, left time.Duration)

type Calendar struct {
	path string
	http *http.Client
	log  *slog.Logger
	Warn Warn
	now  func() time.Time

	mu      sync.Mutex
	saved   saved
	events  []Event
	fetched time.Time
	lastErr string
	warned  map[string]time.Time
	wake    chan struct{}
}

// Open reads the saved address from path; a missing file is a calendar not set up yet.
func Open(path string, log *slog.Logger) (*Calendar, error) {
	c := &Calendar{path: path, http: &http.Client{Timeout: 30 * time.Second}, log: log, now: time.Now,
		warned: map[string]time.Time{}, wake: make(chan struct{}, 1)}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return c, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c.saved); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return c, nil
}

func (c *Calendar) lead() time.Duration {
	if c.saved.LeadMinutes <= 0 {
		return DefaultLead
	}
	return time.Duration(c.saved.LeadMinutes) * time.Minute
}

// Status never includes the address: whoever has it can read the whole calendar.
func (c *Calendar) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{Configured: c.saved.URL != "", Account: owner(c.saved.URL), LeadMinutes: int(c.lead() / time.Minute),
		FetchedAt: c.fetched, Error: c.lastErr, Next: []Event{}}
	now := c.now()
	for _, e := range c.events {
		if len(s.Next) == shownNext {
			break
		}
		if e.End.After(now) {
			s.Next = append(s.Next, e)
		}
	}
	return s
}

// Next is the first event still to start, if there is one.
func (c *Calendar) Next() (Event, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for _, e := range c.events {
		if e.Start.After(now) {
			return e, true
		}
	}
	return Event{}, false
}

// Set checks that address serves a calendar before keeping it; a zero lead keeps the current one.
func (c *Calendar) Set(ctx context.Context, address string, leadMinutes int) error {
	if leadMinutes < 0 || time.Duration(leadMinutes)*time.Minute > MaxLead {
		return fmt.Errorf("lead_minutes %d: want 1 to %d", leadMinutes, int(MaxLead/time.Minute))
	}
	c.mu.Lock()
	next := c.saved
	c.mu.Unlock()
	if leadMinutes > 0 {
		next.LeadMinutes = leadMinutes
	}
	var events []Event
	if address != "" {
		u, err := normalize(address)
		if err != nil {
			return err
		}
		if events, err = c.fetch(ctx, u); err != nil {
			return err
		}
		next.URL = u
	}
	if next.URL == "" {
		return ErrNotConfigured
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.saveLocked(next); err != nil {
		return err
	}
	if address != "" {
		c.events, c.fetched, c.lastErr = events, c.now(), ""
	}
	c.poke()
	return nil
}

func (c *Calendar) Forget() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	c.saved, c.events, c.fetched, c.lastErr = saved{}, nil, time.Time{}, ""
	return nil
}

// Run reads the calendar every 5 minutes and warns before each event.
func (c *Calendar) Run(ctx context.Context) {
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		c.mu.Lock()
		address, due := c.saved.URL, c.now().Sub(c.fetched) >= fetchEvery
		c.mu.Unlock()
		if address != "" && due {
			events, err := c.fetch(ctx, address)
			c.mu.Lock()
			if c.saved.URL == address {
				c.fetched = c.now()
				if err != nil {
					c.lastErr = err.Error()
					c.log.Warn("calendar", "err", err)
				} else {
					c.events, c.lastErr = events, ""
				}
			}
			c.mu.Unlock()
		}
		c.warn()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-c.wake:
		}
	}
}

func (c *Calendar) warn() {
	c.mu.Lock()
	now := c.now()
	lead := c.lead()
	var due []Event
	for _, e := range c.events {
		key := e.uid + "@" + e.Start.UTC().Format(time.RFC3339)
		left := e.Start.Sub(now)
		if left > 0 && left <= lead && c.warned[key].IsZero() {
			c.warned[key] = e.Start
			due = append(due, e)
		}
	}
	for key, start := range c.warned {
		if now.Sub(start) > time.Hour {
			delete(c.warned, key)
		}
	}
	warn := c.Warn
	c.mu.Unlock()
	for _, e := range due {
		c.log.Info("calendar", "event", e.Title, "start", e.Start)
		if warn != nil {
			warn(e, e.Start.Sub(now))
		}
	}
}

func (c *Calendar) fetch(ctx context.Context, address string) ([]Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		// The error carries the address, which is a secret.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("read the calendar: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read the calendar: HTTP %d", res.StatusCode)
	}
	vevents, err := parse(io.LimitReader(res.Body, maxFeedSize), owner(address))
	if err != nil {
		return nil, fmt.Errorf("read the calendar: %w", err)
	}
	now := c.now()
	return occurrences(vevents, now.Add(-24*time.Hour), now.Add(lookAhead)), nil
}

// normalize takes the address as calendars hand it out, webcal:// included.
func normalize(address string) (string, error) {
	address = strings.TrimSpace(address)
	if rest, ok := strings.CutPrefix(address, "webcal://"); ok {
		address = "https://" + rest
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("want the calendar's iCal address, starting with https:// or webcal://")
	}
	return u.String(), nil
}

// owner is the account in a Google Calendar address (/calendar/ical/<account>/private-.../basic.ics).
func owner(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "calendar" && parts[1] == "ical" {
		if acct, err := url.PathUnescape(parts[2]); err == nil && strings.Contains(acct, "@") {
			return strings.ToLower(acct)
		}
	}
	return ""
}

func (c *Calendar) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Calendar) saveLocked(s saved) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(c.path, data); err != nil {
		return err
	}
	c.saved = s
	return nil
}
