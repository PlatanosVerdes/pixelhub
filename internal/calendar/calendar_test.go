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

func TestWarnsOnceWithinTheLead(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	soon, later := now.Add(8*time.Minute), now.Add(2*time.Hour)
	srv := serveFeed(t, fmt.Sprintf("BEGIN:VCALENDAR\r\n"+
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART:%s\r\nSUMMARY:Soon\r\nEND:VEVENT\r\n"+
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART:%s\r\nSUMMARY:Later\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		soon.Format("20060102T150405Z"), later.Format("20060102T150405Z")))
	path := filepath.Join(t.TempDir(), "calendar.json")
	c, err := Open(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return now }
	var warned []string
	c.Warn = func(e Event, left time.Duration) { warned = append(warned, fmt.Sprintf("%s %s", e.Title, left)) }
	if err := c.Set(context.Background(), "webcal"+strings.TrimPrefix(srv.URL, "http"), 0); err == nil {
		t.Fatal("webcal:// becomes https://, which the test server does not speak; want an error")
	}
	if err := c.Set(context.Background(), srv.URL, 0); err != nil {
		t.Fatal(err)
	}
	c.warn()
	c.warn()
	if strings.Join(warned, ",") != "Soon 8m0s" {
		t.Errorf("warned %v, want Soon once", warned)
	}
	if s := c.Status(); !s.Configured || s.LeadMinutes != 10 || len(s.Next) != 2 {
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
	for _, left := range []time.Duration{30 * time.Second, 9 * time.Minute, 10 * time.Minute, 3 * time.Hour} {
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
