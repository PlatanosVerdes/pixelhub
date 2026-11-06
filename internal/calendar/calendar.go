// Package calendar links a Google account, follows the calendars picked from it, and warns on the
// display at each event's reminders and when it starts.
package calendar

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
)

const (
	scope      = "https://www.googleapis.com/auth/calendar.readonly"
	pendingFor = 10 * time.Minute
	fetchEvery = 5 * time.Minute
	listEvery  = 30 * time.Minute
	checkEvery = 20 * time.Second
	lookAhead  = 8 * 24 * time.Hour
	maxPages   = 4
	shownNext  = 40
	// startGrace is how late the warning at the start of an event may still go out, after a restart.
	startGrace = 2 * time.Minute
)

var (
	ErrNotConfigured = errors.New("no Google client yet")
	ErrUnknownLogin  = errors.New("this sign-in was not started here or took too long; start it again")
)

// Event is one occurrence of an event, recurring ones already expanded by Google.
type Event struct {
	Title    string    `json:"title"`
	Calendar string    `json:"calendar"`
	Color    string    `json:"color,omitempty"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	// Alarms are the event's reminders, its calendar's defaults included, as minutes before it starts.
	Alarms []int `json:"alarms"`
	id     string
}

type Calendar struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
	Selected bool   `json:"selected"`
	defaults []int
}

type Status struct {
	Configured bool       `json:"configured"`
	ClientID   string     `json:"client_id,omitempty"`
	Connected  bool       `json:"connected"`
	Account    string     `json:"account,omitempty"`
	Calendars  []Calendar `json:"calendars"`
	Next       []Event    `json:"next"`
	FetchedAt  time.Time  `json:"fetched_at,omitzero"`
	Error      string     `json:"error,omitempty"`
}

type saved struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Refresh      string   `json:"refresh_token,omitempty"`
	Account      string   `json:"account,omitempty"`
	Calendars    []string `json:"calendars,omitempty"`
}

type login struct {
	verifier, redirect string
	at                 time.Time
}

// Warn is called at each of an event's reminders, with the time left, and at its start with zero.
type Warn func(e Event, left time.Duration)

type Account struct {
	path string
	http *http.Client
	log  *slog.Logger
	Warn Warn
	now  func() time.Time
	// AuthURL, TokenURL and APIURL are Google's, replaced in tests.
	AuthURL, TokenURL, APIURL string

	mu        sync.Mutex
	saved     saved
	access    string
	expires   time.Time
	pending   map[string]login
	calendars []Calendar
	listed    time.Time
	events    []Event
	fetched   time.Time
	lastErr   string
	warned    map[string]time.Time
	wake      chan struct{}
}

// Open reads the saved link from path; a missing file is an account not set up yet.
func Open(path string, log *slog.Logger) (*Account, error) {
	a := &Account{
		path: path, http: &http.Client{Timeout: 30 * time.Second}, log: log, now: time.Now,
		AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
		APIURL:  "https://www.googleapis.com/calendar/v3",
		pending: map[string]login{}, warned: map[string]time.Time{}, wake: make(chan struct{}, 1),
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return a, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &a.saved); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return a, nil
}

// Status never includes the client secret or the tokens.
func (a *Account) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Status{Configured: a.saved.ClientID != "", ClientID: a.saved.ClientID, Connected: a.saved.Refresh != "",
		Account: a.saved.Account, Calendars: slices.Clone(a.calendars), Next: []Event{}, FetchedAt: a.fetched, Error: a.lastErr}
	if s.Calendars == nil {
		s.Calendars = []Calendar{}
	}
	now := a.now()
	for _, e := range a.events {
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
func (a *Account) Next() (Event, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for _, e := range a.events {
		if e.Start.After(now) {
			return e, true
		}
	}
	return Event{}, false
}

// SetClient keeps the OAuth client of the user's Google Cloud project; a new one forgets the account.
func (a *Account) SetClient(id, secret string) error {
	id, secret = strings.TrimSpace(id), strings.TrimSpace(secret)
	if !strings.HasSuffix(id, ".apps.googleusercontent.com") {
		return fmt.Errorf("client ID %q: want the one ending in .apps.googleusercontent.com", id)
	}
	if secret == "" {
		return errors.New("client secret: paste the one shown next to the client ID")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if id == a.saved.ClientID && secret == a.saved.ClientSecret {
		return nil
	}
	a.access, a.calendars, a.events, a.lastErr = "", nil, nil, ""
	return a.saveLocked(saved{ClientID: id, ClientSecret: secret})
}

// Select picks the calendars to follow, by id.
func (a *Account) Select(ids []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.saved.Refresh == "" {
		return errors.New("connect a Google account first")
	}
	next := a.saved
	next.Calendars = slices.Compact(slices.Sorted(slices.Values(ids)))
	if err := a.saveLocked(next); err != nil {
		return err
	}
	for i := range a.calendars {
		a.calendars[i].Selected = slices.Contains(next.Calendars, a.calendars[i].ID)
	}
	a.fetched = time.Time{}
	a.poke()
	return nil
}

func (a *Account) Forget() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.access, a.calendars, a.events, a.lastErr, a.fetched, a.listed = "", nil, nil, "", time.Time{}, time.Time{}
	return a.saveLocked(saved{ClientID: a.saved.ClientID, ClientSecret: a.saved.ClientSecret})
}

// LoginURL starts a sign-in that comes back to redirect, which must be listed in the OAuth client.
func (a *Account) LoginURL(redirect string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.saved.ClientID == "" {
		return "", ErrNotConfigured
	}
	state, verifier := random(16), random(48)
	for k, l := range a.pending {
		if time.Since(l.at) > pendingFor {
			delete(a.pending, k)
		}
	}
	a.pending[state] = login{verifier: verifier, redirect: redirect, at: time.Now()}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id": {a.saved.ClientID}, "response_type": {"code"}, "redirect_uri": {redirect}, "scope": {scope},
		// Without consent Google only sends a refresh token the first time the account allows the app.
		"access_type": {"offline"}, "prompt": {"consent"},
		"state": {state}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
	}
	return a.AuthURL + "?" + q.Encode(), nil
}

// Finish trades the code Google sent back for a refresh token, and follows the primary calendar.
func (a *Account) Finish(ctx context.Context, state, code string) error {
	a.mu.Lock()
	l, ok := a.pending[state]
	delete(a.pending, state)
	client := a.saved
	a.mu.Unlock()
	if !ok || time.Since(l.at) > pendingFor {
		return ErrUnknownLogin
	}
	tok, err := a.token(ctx, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {l.redirect},
		"client_id": {client.ClientID}, "client_secret": {client.ClientSecret}, "code_verifier": {l.verifier},
	})
	if err != nil {
		return err
	}
	if tok.Refresh == "" {
		return errors.New("Google sent no refresh token; remove pixelhub's access in your Google account and connect again")
	}
	a.mu.Lock()
	a.access, a.expires = tok.Access, a.now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	err = a.saveLocked(saved{ClientID: client.ClientID, ClientSecret: client.ClientSecret, Refresh: tok.Refresh})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	cals, err := a.listCalendars(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.saved
	for _, c := range cals {
		if c.Primary {
			next.Account, next.Calendars = c.ID, []string{c.ID}
		}
	}
	if err := a.saveLocked(next); err != nil {
		return err
	}
	a.setCalendarsLocked(cals)
	a.poke()
	return nil
}

// Run reads the calendars every 5 minutes while an account is linked, and warns before each event.
func (a *Account) Run(ctx context.Context) {
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		a.refresh(ctx)
		a.warn()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-a.wake:
		}
	}
}

func (a *Account) refresh(ctx context.Context) {
	a.mu.Lock()
	linked := a.saved.Refresh != ""
	now := a.now()
	needList, needEvents := now.Sub(a.listed) >= listEvery, now.Sub(a.fetched) >= fetchEvery
	a.mu.Unlock()
	if !linked || (!needList && !needEvents) {
		return
	}
	err := a.read(ctx, needList)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fetched = a.now()
	if err != nil {
		a.lastErr = err.Error()
		a.log.Warn("calendar", "err", err)
		return
	}
	a.lastErr = ""
}

func (a *Account) read(ctx context.Context, list bool) error {
	if list {
		cals, err := a.listCalendars(ctx)
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.setCalendarsLocked(cals)
		a.mu.Unlock()
	}
	a.mu.Lock()
	var picked []Calendar
	for _, c := range a.calendars {
		if c.Selected {
			picked = append(picked, c)
		}
	}
	a.mu.Unlock()
	var all []Event
	for _, c := range picked {
		events, err := a.listEvents(ctx, c)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		all = append(all, events...)
	}
	slices.SortFunc(all, func(x, y Event) int { return x.Start.Compare(y.Start) })
	a.mu.Lock()
	a.events = all
	a.mu.Unlock()
	return nil
}

func (a *Account) setCalendarsLocked(cals []Calendar) {
	for i := range cals {
		cals[i].Selected = slices.Contains(a.saved.Calendars, cals[i].ID)
	}
	a.calendars, a.listed = cals, a.now()
}

func (a *Account) listCalendars(ctx context.Context) ([]Calendar, error) {
	var res struct {
		Items []struct {
			ID              string     `json:"id"`
			Summary         string     `json:"summary"`
			SummaryOverride string     `json:"summaryOverride"`
			Color           string     `json:"backgroundColor"`
			Primary         bool       `json:"primary"`
			Reminders       []reminder `json:"defaultReminders"`
		} `json:"items"`
	}
	if err := a.get(ctx, "/users/me/calendarList", nil, &res); err != nil {
		return nil, err
	}
	cals := make([]Calendar, 0, len(res.Items))
	for _, it := range res.Items {
		cals = append(cals, Calendar{ID: it.ID, Name: cmp.Or(it.SummaryOverride, it.Summary, it.ID), Color: it.Color,
			Primary: it.Primary, defaults: minutes(it.Reminders)})
	}
	// The primary calendar first, then by name, as Google's own list shows them.
	slices.SortStableFunc(cals, func(x, y Calendar) int {
		if x.Primary != y.Primary {
			if x.Primary {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return cals, nil
}

type reminder struct {
	Minutes int `json:"minutes"`
}

func minutes(rs []reminder) []int {
	out := []int{}
	for _, r := range rs {
		if r.Minutes > 0 && !slices.Contains(out, r.Minutes) {
			out = append(out, r.Minutes)
		}
	}
	slices.Sort(out)
	return out
}

type attendee struct {
	Self           bool   `json:"self"`
	ResponseStatus string `json:"responseStatus"`
}

type apiEvent struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	EventType string `json:"eventType"`
	Start     struct {
		DateTime time.Time `json:"dateTime"`
	} `json:"start"`
	End struct {
		DateTime time.Time `json:"dateTime"`
	} `json:"end"`
	Reminders struct {
		UseDefault bool       `json:"useDefault"`
		Overrides  []reminder `json:"overrides"`
	} `json:"reminders"`
	Attendees []attendee `json:"attendees"`
}

// listEvents reads the timed events of a calendar in the next days, recurring ones expanded, without
// the cancelled, the declined, and those that are not meetings (working location, out of office).
func (a *Account) listEvents(ctx context.Context, c Calendar) ([]Event, error) {
	now := a.now()
	q := url.Values{
		"singleEvents": {"true"}, "orderBy": {"startTime"}, "maxResults": {"250"},
		"timeMin": {now.Add(-time.Hour).UTC().Format(time.RFC3339)}, "timeMax": {now.Add(lookAhead).UTC().Format(time.RFC3339)},
	}
	var out []Event
	for range maxPages {
		var res struct {
			Items []apiEvent `json:"items"`
			Next  string     `json:"nextPageToken"`
		}
		if err := a.get(ctx, "/calendars/"+url.PathEscape(c.ID)+"/events", q, &res); err != nil {
			return nil, err
		}
		for _, e := range res.Items {
			declined := slices.ContainsFunc(e.Attendees, func(at attendee) bool { return at.Self && at.ResponseStatus == "declined" })
			if e.Status == "cancelled" || declined || e.Start.DateTime.IsZero() ||
				(e.EventType != "" && e.EventType != "default" && e.EventType != "fromGmail") {
				continue
			}
			alarms := minutes(e.Reminders.Overrides)
			if e.Reminders.UseDefault {
				alarms = slices.Clone(c.defaults)
			}
			out = append(out, Event{Title: e.Summary, Calendar: c.Name, Color: c.Color, Start: e.Start.DateTime, End: e.End.DateTime,
				Alarms: alarms, id: c.ID + "/" + e.ID})
		}
		if res.Next == "" {
			break
		}
		q.Set("pageToken", res.Next)
	}
	return out, nil
}

// warn goes through each event's reminders and its start. Reminders missed while pixelhub was down
// collapse into one, as long as the event has not started.
func (a *Account) warn() {
	type due struct {
		e    Event
		left time.Duration
	}
	a.mu.Lock()
	now := a.now()
	var fire []due
	for _, e := range a.events {
		key := e.id + "@" + e.Start.UTC().Format(time.RFC3339) + "@"
		if !now.Before(e.Start) {
			if now.Sub(e.Start) < startGrace && a.mark(key+"0", e.Start) {
				fire = append(fire, due{e, 0})
			}
			continue
		}
		fired := false
		for _, m := range e.Alarms {
			if !now.Before(e.Start.Add(-time.Duration(m)*time.Minute)) && a.mark(key+strconv.Itoa(m), e.Start) {
				fired = true
			}
		}
		if fired {
			fire = append(fire, due{e, e.Start.Sub(now)})
		}
	}
	for key, start := range a.warned {
		if now.Sub(start) > time.Hour {
			delete(a.warned, key)
		}
	}
	warn := a.Warn
	a.mu.Unlock()
	for _, d := range fire {
		a.log.Info("calendar", "event", d.e.Title, "start", d.e.Start, "left", d.left.Round(time.Second))
		if warn != nil {
			warn(d.e, d.left)
		}
	}
}

// mark records a warning as given and reports whether it was new.
func (a *Account) mark(key string, start time.Time) bool {
	if !a.warned[key].IsZero() {
		return false
	}
	a.warned[key] = start
	return true
}

func (a *Account) get(ctx context.Context, path string, q url.Values, v any) error {
	access, err := a.accessToken(ctx)
	if err != nil {
		return err
	}
	u := a.APIURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		a.mu.Lock()
		a.access = ""
		a.mu.Unlock()
		return errors.New("Google answered 401")
	case res.StatusCode != http.StatusOK:
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
		return fmt.Errorf("Google answered %d: %s", res.StatusCode, cmp.Or(body.Error.Message, res.Status))
	}
	return json.NewDecoder(res.Body).Decode(v)
}

func (a *Account) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.access != "" && a.expires.Sub(a.now()) > time.Minute {
		defer a.mu.Unlock()
		return a.access, nil
	}
	s := a.saved
	a.mu.Unlock()
	if s.Refresh == "" {
		return "", errors.New("not signed in")
	}
	tok, err := a.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.Refresh},
		"client_id": {s.ClientID}, "client_secret": {s.ClientSecret}})
	var grant *grantError
	if errors.As(err, &grant) {
		// The refresh token expired or was revoked: drop it so the page offers to connect again.
		a.mu.Lock()
		defer a.mu.Unlock()
		a.log.Warn("Google sign-in no longer valid, connect again", "err", err)
		a.calendars, a.events = nil, nil
		if saveErr := a.saveLocked(saved{ClientID: s.ClientID, ClientSecret: s.ClientSecret}); saveErr != nil {
			return "", saveErr
		}
		return "", err
	}
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.access, a.expires = tok.Access, a.now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	return a.access, nil
}

type grantError struct{ detail string }

func (g *grantError) Error() string { return "token: " + g.detail }

type token struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
	Error     string `json:"error"`
	Detail    string `json:"error_description"`
}

func (a *Account) token(ctx context.Context, form url.Values) (token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := a.http.Do(req)
	if err != nil {
		return token{}, err
	}
	defer res.Body.Close()
	var tok token
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&tok); err != nil {
		return token{}, fmt.Errorf("token: HTTP %d", res.StatusCode)
	}
	if tok.Error == "invalid_grant" {
		return token{}, &grantError{cmp.Or(tok.Detail, tok.Error)}
	}
	if res.StatusCode != http.StatusOK || tok.Access == "" {
		return token{}, fmt.Errorf("token: %s", cmp.Or(tok.Detail, tok.Error, res.Status))
	}
	return tok, nil
}

func (a *Account) poke() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *Account) saveLocked(s saved) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(a.path, data); err != nil {
		return err
	}
	a.saved = s
	return nil
}

func random(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
