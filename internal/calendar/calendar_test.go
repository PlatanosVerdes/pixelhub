package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

// fakeGoogle hands out tokens and serves two calendars: the primary one with a 30 minute default
// reminder, and a shared one.
type fakeGoogle struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	revoked bool
	asked   []string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("client_secret") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
		switch {
		case r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code") == "code" && r.Form.Get("code_verifier") != "":
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
		case r.Form.Get("grant_type") == "refresh_token" && !f.revoked:
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "expires_in": 3600})
		default:
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		}
	})
	mux.HandleFunc("GET /api/users/me/calendarList", func(w http.ResponseWriter, r *http.Request) {
		f.check(r)
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"id": "shared@group.calendar.google.com", "summary": "Agenda de Dos"},
			{"id": "me@gmail.com", "summary": "me@gmail.com", "summaryOverride": "Jorge", "primary": true,
				"defaultReminders": []map[string]any{{"method": "popup", "minutes": 30}}},
		}})
	})
	mux.HandleFunc("GET /api/calendars/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		f.check(r)
		f.mu.Lock()
		f.asked = append(f.asked, r.PathValue("id"))
		f.mu.Unlock()
		at := func(d time.Duration) map[string]any {
			return map[string]any{"dateTime": now.Add(d).Format(time.RFC3339)}
		}
		items := []map[string]any{}
		switch r.PathValue("id") {
		case "me@gmail.com":
			items = []map[string]any{
				{"id": "a", "summary": "Defaults", "start": at(40 * time.Minute), "end": at(time.Hour), "reminders": map[string]any{"useDefault": true}},
				{"id": "b", "summary": "Own", "start": at(20 * time.Minute), "end": at(time.Hour),
					"reminders": map[string]any{"overrides": []map[string]any{{"method": "popup", "minutes": 10}, {"method": "email", "minutes": 10}}}},
				{"id": "c", "summary": "Declined", "start": at(15 * time.Minute), "end": at(time.Hour), "reminders": map[string]any{"useDefault": true},
					"attendees": []map[string]any{{"email": "me@gmail.com", "self": true, "responseStatus": "declined"}}},
				{"id": "d", "summary": "Holiday", "start": map[string]any{"date": "2026-11-06"}, "end": map[string]any{"date": "2026-11-07"}},
				{"id": "e", "summary": "Office", "eventType": "workingLocation", "start": at(5 * time.Minute), "end": at(time.Hour)},
			}
		case "shared@group.calendar.google.com":
			items = []map[string]any{{"id": "s", "summary": "Shared", "start": at(30 * time.Minute), "end": at(time.Hour)}}
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGoogle) check(r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer access" {
		f.t.Errorf("%s without the access token", r.URL.Path)
	}
}

func open(t *testing.T, f *fakeGoogle) (*Account, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "google.json")
	a, err := Open(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	a.AuthURL, a.TokenURL, a.APIURL = f.srv.URL+"/auth", f.srv.URL+"/token", f.srv.URL+"/api"
	a.now = func() time.Time { return now }
	return a, path
}

func signIn(t *testing.T, a *Account) {
	t.Helper()
	if err := a.SetClient("123-abc.apps.googleusercontent.com", "secret"); err != nil {
		t.Fatal(err)
	}
	to, err := a.LoginURL("https://pixelhub.example/api/google/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(to)
	if q := u.Query(); q.Get("access_type") != "offline" || q.Get("scope") != scope || q.Get("code_challenge") == "" {
		t.Errorf("login URL %s", to)
	}
	if err := a.Finish(context.Background(), u.Query().Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
}

func TestSignInFollowsThePrimaryCalendar(t *testing.T) {
	t.Parallel()
	f := newFakeGoogle(t)
	a, path := open(t, f)
	signIn(t, a)
	a.refresh(context.Background())
	s := a.Status()
	if !s.Connected || s.Account != "me@gmail.com" || len(s.Calendars) != 2 || s.Calendars[0].Name != "Jorge" || !s.Calendars[0].Selected || s.Calendars[1].Selected {
		t.Fatalf("status %+v", s)
	}
	var got []string
	for _, e := range s.Next {
		got = append(got, fmt.Sprintf("%s %v", e.Title, e.Alarms))
	}
	if strings.Join(got, ", ") != "Own [10], Defaults [30]" {
		t.Errorf("events %v", got)
	}
	data, _ := os.ReadFile(path)
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 || !strings.Contains(string(data), "refresh") {
		t.Errorf("saved %s with mode %v", data, info.Mode())
	}
	if b, _ := json.Marshal(s); strings.Contains(string(b), "secret") || strings.Contains(string(b), "refresh") {
		t.Errorf("status leaks the secret or the token: %s", b)
	}

	if err := a.Select([]string{"shared@group.calendar.google.com", "me@gmail.com"}); err != nil {
		t.Fatal(err)
	}
	a.refresh(context.Background())
	if n := len(a.Status().Next); n != 3 {
		t.Errorf("%d events from both calendars, want 3", n)
	}
	reopened, _ := Open(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(reopened.saved.Calendars) != 2 {
		t.Errorf("picked calendars not kept: %v", reopened.saved.Calendars)
	}
	if err := a.Forget(); err != nil || a.Status().Connected || !a.Status().Configured {
		t.Errorf("forget: %v %+v", err, a.Status())
	}
}

func TestWarnsAtEachReminderAndAtTheStart(t *testing.T) {
	t.Parallel()
	f := newFakeGoogle(t)
	a, _ := open(t, f)
	signIn(t, a)
	clock := now
	a.now = func() time.Time { return clock }
	var warned []string
	a.Warn = func(e Event, left time.Duration) { warned = append(warned, fmt.Sprintf("%s %s", e.Title, left)) }
	a.refresh(context.Background())
	for _, step := range []time.Duration{0, 10 * time.Minute, 10 * time.Minute, 20 * time.Minute, time.Minute} {
		clock = clock.Add(step)
		a.warn()
	}
	want := []string{
		"Own 10m0s",      // 10:10, its own reminder
		"Defaults 30m0s", // 10:10, the calendar's default
		"Own 0s",         // 10:20, at the start
		"Defaults 0s",    // 10:40
	}
	if strings.Join(warned, ", ") != strings.Join(want, ", ") {
		t.Errorf("warned\n %v\nwant\n %v", warned, want)
	}
}

func TestMissedRemindersCollapseIntoOne(t *testing.T) {
	t.Parallel()
	c, _ := Open(filepath.Join(t.TempDir(), "google.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.now = func() time.Time { return now }
	c.events = []Event{
		{Title: "Soon", Start: now.Add(3 * time.Minute), Alarms: []int{5, 30}, id: "a"},
		{Title: "Started", Start: now.Add(-10 * time.Minute), id: "b"},
	}
	var warned []string
	c.Warn = func(e Event, left time.Duration) { warned = append(warned, fmt.Sprintf("%s %s", e.Title, left)) }
	c.warn()
	c.warn()
	if strings.Join(warned, ", ") != "Soon 3m0s" {
		t.Errorf("warned %v, want one warning for Soon and none for what already started", warned)
	}
}

func TestRevokedAccessAsksToConnectAgain(t *testing.T) {
	t.Parallel()
	f := newFakeGoogle(t)
	a, _ := open(t, f)
	signIn(t, a)
	a.mu.Lock()
	a.access = ""
	a.mu.Unlock()
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	a.listed = time.Time{}
	a.refresh(context.Background())
	if s := a.Status(); s.Connected || !s.Configured || s.Error == "" {
		t.Errorf("after a revoked refresh token: %+v", s)
	}
}

func TestRejectsAClientThatIsNotGoogles(t *testing.T) {
	t.Parallel()
	a, _ := Open(filepath.Join(t.TempDir(), "google.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := a.SetClient("abc", "secret"); err == nil {
		t.Error("a client ID without .apps.googleusercontent.com was accepted")
	}
	if err := a.SetClient("1.apps.googleusercontent.com", " "); err == nil {
		t.Error("an empty secret was accepted")
	}
	if _, err := a.LoginURL("https://x/cb"); err != ErrNotConfigured {
		t.Errorf("login without a client: %v", err)
	}
}

func TestPicture(t *testing.T) {
	t.Parallel()
	for _, left := range []time.Duration{0, 30 * time.Second, 9 * time.Minute, 10 * time.Minute, 3 * time.Hour, 2 * 24 * time.Hour} {
		c := Picture(left, 16)
		if len(c.Frames) != 2 {
			t.Fatalf("%s: %d frames", left, len(c.Frames))
		}
		if os.Getenv("CALENDAR_PNG") != "" {
			f, _ := os.Create(filepath.Join(os.Getenv("CALENDAR_PNG"), c.Key+".png"))
			png.Encode(f, c.Frames[0].Image)
			f.Close()
		}
	}
}
