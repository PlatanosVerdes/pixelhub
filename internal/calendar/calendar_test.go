package calendar

import (
	"context"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func serveFeed(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWarnsAtEachReminderAndAtTheStart(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	stamp := func(d time.Duration) string { return now.Add(d).Format("20060102T150405Z") }
	srv := serveFeed(t, "BEGIN:VCALENDAR\r\n"+
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART:"+stamp(40*time.Minute)+"\r\nSUMMARY:Reminded\r\n"+
		"BEGIN:VALARM\r\nTRIGGER:-PT30M\r\nEND:VALARM\r\nBEGIN:VALARM\r\nTRIGGER:-PT5M\r\nEND:VALARM\r\nEND:VEVENT\r\n"+
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART:"+stamp(20*time.Minute)+"\r\nSUMMARY:Plain\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	path := filepath.Join(t.TempDir(), "calendar.json")
	c, err := Open(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	clock := now
	c.now = func() time.Time { return clock }
	var warned []string
	c.Warn = func(e Event, left time.Duration) { warned = append(warned, fmt.Sprintf("%s %s", e.Title, left)) }
	if err := c.Set(context.Background(), srv.URL, 0); err != nil {
		t.Fatal(err)
	}
	for _, step := range []time.Duration{0, 10 * time.Minute, 10 * time.Minute, 15 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		clock = clock.Add(step)
		c.warn()
	}
	want := []string{
		"Plain 10m0s",    // 10:10, the default 10 min
		"Reminded 30m0s", // 10:10, its own 30 min reminder
		"Plain 0s",       // 10:20, at the start
		"Reminded 5m0s",  // 10:35
		"Reminded 0s",    // 10:40
	}
	if strings.Join(warned, ", ") != strings.Join(want, ", ") {
		t.Errorf("warned\n %v\nwant\n %v", warned, want)
	}
	if s := c.Status(); !s.Configured || s.LeadMinutes != 10 {
		t.Errorf("status %+v", s)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("saved address: %v, mode %v; want it readable only by pixelhub", err, info.Mode())
	}
	reopened, err := Open(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || !reopened.Status().Configured {
		t.Errorf("reopened: %v %+v", err, reopened.Status())
	}
	if err := c.Forget(); err != nil || c.Status().Configured {
		t.Errorf("forget: %v %+v", err, c.Status())
	}
}

func TestMissedRemindersCollapseIntoOne(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	c, _ := Open(filepath.Join(t.TempDir(), "calendar.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.now = func() time.Time { return now }
	c.events = []Event{
		{Title: "Soon", Start: now.Add(3 * time.Minute), Alarms: []int{5, 30}, uid: "a"},
		{Title: "Started", Start: now.Add(-10 * time.Minute), uid: "b"},
	}
	var warned []string
	c.Warn = func(e Event, left time.Duration) { warned = append(warned, fmt.Sprintf("%s %s", e.Title, left)) }
	c.warn()
	c.warn()
	if strings.Join(warned, ", ") != "Soon 3m0s" {
		t.Errorf("warned %v, want one warning for Soon and none for what already started", warned)
	}
}

func TestRejectsWhatIsNotACalendar(t *testing.T) {
	t.Parallel()
	srv := serveFeed(t, "<html>sign in</html>")
	c, _ := Open(filepath.Join(t.TempDir(), "calendar.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, address := range []string{srv.URL, "ftp://example.com/x.ics", "not a url"} {
		if err := c.Set(context.Background(), address, 0); err == nil {
			t.Errorf("%s: accepted", address)
		}
	}
	if err := c.Set(context.Background(), "", 90); err == nil {
		t.Error("a 90 minute lead was accepted")
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
