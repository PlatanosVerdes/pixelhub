package state

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var madrid = mustLocation("Europe/Madrid")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

const sample = `{
  "location": {"latitude": 10, "longitude": 20},
  "brightness": {"day": 70, "dusk": 30, "night": 5},
  "release_for": "2h",
  "default_channel": "evening",
  "channels": [
    {"name": "office", "sources": ["office", "system"], "ambient": {"pictures": "2m", "clock": "3m"}, "scenes": []},
    {"name": "evening", "sources": ["*"], "ambient": {"pictures": "0s", "clock": "1m"},
     "scenes": [{"name": "sleep", "days": "mon-fri", "from": "00:00", "to": "07:30", "sprite": "zzz", "brightness": "off"}]}
  ],
  "schedule": [{"channel": "office", "days": "mon-fri", "from": "09:00", "to": "18:00"}]
}`

func mustDecode(t *testing.T, data string) State {
	t.Helper()
	s, err := Decode([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDecode(t *testing.T) {
	t.Parallel()
	s := mustDecode(t, sample)
	evening, _ := s.Channel("evening")
	if sc := evening.Scenes[0]; *sc.Brightness != 0 || sc.From != 0 || sc.To != 7*60+30 || !sc.Days.Has(time.Monday) || sc.Days.Has(time.Sunday) {
		t.Errorf("scene %+v", sc)
	}
	if time.Duration(s.ReleaseFor) != 2*time.Hour || s.ScheduledChannel(time.Date(2026, 11, 6, 10, 0, 0, 0, madrid)) != "office" {
		t.Errorf("state %+v", s)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	want := mustDecode(t, sample)
	store, _, err := Open(path, discard())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	_, got, err := Open(path, discard())
	if err != nil {
		t.Fatal(err)
	}
	if a, b := encode(t, got), encode(t, want); a != b {
		t.Errorf("round trip changed the state\n got %s\nwant %s", a, b)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left %d files behind, want only state.json", len(entries))
	}
}

func TestStoreStartsFromDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		aside   bool
	}{
		{"missing file", "", false},
		{"not json", "{nope", true},
		{"invalid state", `{"default_channel": "gone", "channels": []}`, true},
		{"unknown field", `{"colour": 1}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if tt.content != "" {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, s, err := Open(path, discard())
			if err != nil {
				t.Fatal(err)
			}
			if encode(t, s) != encode(t, Defaults()) {
				t.Errorf("got %+v, want the defaults", s)
			}
			matches, _ := filepath.Glob(path + ".corrupt-*")
			if (len(matches) == 1) != tt.aside {
				t.Errorf("files kept aside: %v", matches)
			}
			if tt.aside {
				if kept, _ := os.ReadFile(matches[0]); string(kept) != tt.content {
					t.Errorf("kept %q", kept)
				}
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	s := Defaults()
	c, ok := s.Channel(s.DefaultChannel)
	if err := s.Validate(); err != nil || len(s.Channels) != 1 || !ok || !c.Accepts("") || c.Ambient.Pictures != 0 || len(s.Schedule) != 0 {
		t.Errorf("defaults %+v: %v", s, err)
	}
	if got := encode(t, s); !strings.Contains(got, `"scenes":[]`) || !strings.Contains(got, `"schedule":[]`) {
		t.Errorf("empty lists should encode as []: %s", got)
	}
}

func TestChannelRules(t *testing.T) {
	t.Parallel()
	office := Channel{Name: "office", Sources: []string{"office"}, Ambient: NewAmbient()}
	tests := []struct {
		name   string
		change func(*State) error
		err    error
		check  func(State) bool
	}{
		{"add", func(s *State) error { return s.AddChannel(Channel{Name: "garden", Ambient: NewAmbient()}) }, nil,
			func(s State) bool { _, ok := s.Channel("garden"); return ok }},
		{"add a duplicate", func(s *State) error { return s.AddChannel(office) }, ErrConflict, nil},
		{"rename updates the schedule", func(s *State) error {
			c := office
			c.Name = "studio"
			return s.ReplaceChannel("office", c)
		}, nil, func(s State) bool {
			_, old := s.Channel("office")
			return !old && s.Schedule[0].Channel == "studio"
		}},
		{"rename the default", func(s *State) error {
			c, _ := s.Channel("evening")
			c.Name = "night"
			return s.ReplaceChannel("evening", c)
		}, nil, func(s State) bool { return s.DefaultChannel == "night" }},
		{"update without name or ambient keeps them", func(s *State) error {
			return s.ReplaceChannel("office", Channel{Sources: []string{"*"}})
		}, nil, func(s State) bool {
			c, ok := s.Channel("office")
			return ok && c.Ambient == NewAmbient() && c.Accepts("anything")
		}},
		{"add without ambient gets the usual one", func(s *State) error { return s.AddChannel(Channel{Name: "garden"}) }, nil,
			func(s State) bool { c, _ := s.Channel("garden"); return c.Ambient == NewAmbient() }},
		{"rename onto another channel", func(s *State) error {
			c := office
			c.Name = "evening"
			return s.ReplaceChannel("office", c)
		}, ErrConflict, nil},
		{"update a missing channel", func(s *State) error { return s.ReplaceChannel("gym", office) }, ErrNotFound, nil},
		{"delete drops its slots", func(s *State) error { return s.DeleteChannel("office") }, nil,
			func(s State) bool { return len(s.Channels) == 1 && len(s.Schedule) == 0 }},
		{"delete the default", func(s *State) error { return s.DeleteChannel("evening") }, ErrConflict, nil},
		{"delete the last", func(s *State) error {
			if err := s.DeleteChannel("office"); err != nil {
				return err
			}
			s.DefaultChannel = "nothing"
			return s.DeleteChannel("evening")
		}, ErrConflict, nil},
		{"delete a missing channel", func(s *State) error { return s.DeleteChannel("gym") }, ErrNotFound, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := mustDecode(t, sample)
			err := tt.change(&s)
			if !errors.Is(err, tt.err) {
				t.Fatalf("error %v, want %v", err, tt.err)
			}
			if tt.check != nil {
				if !tt.check(s) {
					t.Errorf("state after: %+v", s)
				}
				if err := s.Validate(); err != nil {
					t.Errorf("state no longer valid: %v", err)
				}
			}
		})
	}
}

func TestApplySettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		body  string
		err   error
		check func(State) bool
	}{
		{"one nested field keeps the others", `{"brightness": {"night": 1}}`, nil,
			func(s State) bool { return s.Brightness == Levels{Day: 70, Dusk: 30, Night: 1} }},
		{"latitude alone", `{"location": {"latitude": 40.4}}`, nil,
			func(s State) bool { return s.Location == Location{Latitude: 40.4, Longitude: 20} }},
		{"release and default", `{"release_for": "30m", "default_channel": "office"}`, nil,
			func(s State) bool {
				return time.Duration(s.ReleaseFor) == 30*time.Minute && s.DefaultChannel == "office"
			}},
		{"unknown key", `{"brightnes": {}}`, ErrInvalid, nil},
		{"channels are not settings", `{"channels": []}`, ErrInvalid, nil},
		{"bad duration", `{"release_for": "soon"}`, ErrInvalid, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := mustDecode(t, sample)
			err := s.ApplySettings([]byte(tt.body))
			if !errors.Is(err, tt.err) {
				t.Fatalf("error %v, want %v", err, tt.err)
			}
			if tt.check != nil && !tt.check(s) {
				t.Errorf("state after: %+v", s)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*State)
		want   string
	}{
		{"bad channel name", func(s *State) { s.Channels[0].Name = "../x" }, "channel name"},
		{"upper case name", func(s *State) { s.Channels[0].Name = "Office" }, "channel name"},
		{"duplicate channel", func(s *State) { s.Channels[1].Name = "office"; s.DefaultChannel = "office" }, "twice"},
		{"unknown default", func(s *State) { s.DefaultChannel = "gym" }, "default_channel"},
		{"no channels", func(s *State) { s.Channels = nil }, "at least one"},
		{"schedule to a missing channel", func(s *State) { s.Schedule[0].Channel = "gym" }, "schedule[0]"},
		{"empty schedule range", func(s *State) { s.Schedule[0].To = s.Schedule[0].From }, "schedule[0]"},
		{"empty source", func(s *State) { s.Channels[0].Sources = []string{""} }, "empty source"},
		{"short playlist", func(s *State) { s.Channels[0].Ambient = Ambient{Pictures: Duration(5 * time.Second)} }, "10s or more"},
		{"scene without name", func(s *State) { s.Channels[1].Scenes[0].Name = "" }, "no name"},
		{"level out of range", func(s *State) { s.Brightness.Night = 120 }, "brightness.night"},
		{"short release", func(s *State) { s.ReleaseFor = Duration(time.Second) }, "release_for"},
		{"latitude", func(s *State) { s.Location.Latitude = 100 }, "location"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := mustDecode(t, sample)
			tt.change(&s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestDecodeRejects(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, json, want string }{
		{"bad time", `{"from": "7h"}`, "not a time"},
		{"past midnight", `{"from": "24:01"}`, "not a time"},
		{"bad day", `{"days": "mon-fry"}`, "day set"},
		{"bad brightness", `{"brightness": "dim"}`, "0-100"},
		{"too bright", `{"brightness": 101}`, "0-100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var sc Scene
			if err := json.Unmarshal([]byte(tt.json), &sc); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestDaysString(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"mon-fri", "sat,sun", "mon,wed,fri", "daily", "tue-thu,sun", "fri-mon"} {
		d, err := ParseDays(in)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseDays(d.String())
		if err != nil || again != d {
			t.Errorf("%s writes as %s, which reads back as a different set", in, d)
		}
	}
	if d, _ := ParseDays("mon-fri"); d.String() != "mon-fri" {
		t.Errorf("mon-fri writes as %s", d)
	}
}

func encode(t *testing.T, s State) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestParseDays(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []time.Weekday
	}{
		{"mon-fri", []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}},
		{"sat,sun", []time.Weekday{time.Saturday, time.Sunday}},
		{"fri-mon", []time.Weekday{time.Friday, time.Saturday, time.Sunday, time.Monday}},
		{"Wed", []time.Weekday{time.Wednesday}},
		{"daily", []time.Weekday{0, 1, 2, 3, 4, 5, 6}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			d, err := ParseDays(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			var got []time.Weekday
			for w := range time.Weekday(7) {
				if d.Has(w) {
					got = append(got, w)
				}
			}
			if !sameDays(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func sameDays(got, want []time.Weekday) bool {
	set := map[time.Weekday]bool{}
	for _, w := range want {
		set[w] = true
	}
	if len(got) != len(set) {
		return false
	}
	for _, w := range got {
		if !set[w] {
			return false
		}
	}
	return true
}

func TestActiveAt(t *testing.T) {
	t.Parallel()
	weekdays, _ := ParseDays("mon-fri")
	night := Window{Days: weekdays, From: 23 * 60, To: 7*60 + 30}
	morning := Window{Days: weekdays, From: 0, To: 7*60 + 30}
	evening := Window{From: 22 * 60, To: 24 * 60}
	// 2026-11-06 is a Friday.
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, madrid) }
	tests := []struct {
		name   string
		window Window
		at     time.Time
		want   bool
	}{
		{"start is inclusive", morning, at(6, 0, 0), true},
		{"end is exclusive", morning, at(6, 7, 30), false},
		{"last minute", morning, at(6, 7, 29), true},
		{"saturday is not a weekday", morning, at(7, 3, 0), false},
		{"friday night starts", night, at(6, 23, 0), true},
		{"friday night runs into saturday", night, at(7, 3, 0), true},
		{"saturday night does not start", night, at(7, 23, 30), false},
		{"monday small hours belong to sunday", night, at(9, 2, 0), false},
		{"sunday night does not start", night, at(8, 23, 59), false},
		{"monday night runs into tuesday", night, at(10, 7, 0), true},
		{"until midnight", evening, at(6, 23, 59), true},
		{"midnight ends it", evening, at(7, 0, 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.window.ActiveAt(tt.at); got != tt.want {
				t.Errorf("at %s = %v, want %v", tt.at.Format("Mon 15:04"), got, tt.want)
			}
		})
	}
}
