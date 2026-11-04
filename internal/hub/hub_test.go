package hub

import (
	"errors"
	"image"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
)

type fakeDisplay struct {
	mu       sync.Mutex
	shown    []display.Content
	released bool
}

func (d *fakeDisplay) Show(c display.Content) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.shown = append(d.shown, c)
}
func (d *fakeDisplay) Release()               { d.released = true }
func (d *fakeDisplay) Claim()                 { d.released = false }
func (d *fakeDisplay) Status() display.Status { return display.Status{} }

type sprites map[string]bool

func (s sprites) Get(name string) (display.Content, bool) {
	return display.Content{Kind: display.Picture, Name: name}, s[name]
}

// ambientDir serves the same pictures to every channel, with the channel name in front; it cannot
// be changed.
type ambientDir []string

func (ambientDir) List(string) ([]string, error)              { return nil, nil }
func (ambientDir) First(string, string) (image.Image, error)  { return nil, nil }
func (ambientDir) Add(string, string, []byte) (string, error) { return "", nil }
func (ambientDir) Remove(string, string) error                { return nil }
func (ambientDir) RenameChannel(string, string) error         { return nil }
func (ambientDir) DeleteChannel(string) error                 { return nil }

func (a ambientDir) Pick(channel string, turn int) (display.Content, bool, error) {
	if len(a) == 0 {
		return display.Content{}, false, nil
	}
	return display.Content{Kind: display.Picture, Name: channel + "/" + a[turn%len(a)]}, true, nil
}

var madrid = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		panic(err)
	}
	return loc
}()

// 2026-11-06 is a Friday; Barcelona's sun is up from about 07:31 to 17:46.
func friday(hour, minute int) time.Time { return time.Date(2026, 11, 6, hour, minute, 0, 0, madrid) }

const twoChannels = `{
  "location": {"latitude": 41.3874, "longitude": 2.1686},
  "brightness": {"day": 80, "dusk": 40, "night": 10},
  "release_for": "3h",
  "default_channel": "home",
  "schedule": [{"channel": "work", "days": "mon-fri", "from": "09:00", "to": "18:00"}],
  "channels": [
    {"name": "work", "sources": ["work", "system"], "ambient": {"pictures": "2m", "clock": "3m"}, "scenes": []},
    {"name": "home", "sources": ["*"], "ambient": {"pictures": "5m", "clock": "0s"}, "scenes": [
      {"name": "sleep", "days": "mon-fri", "from": "00:00", "to": "07:30", "sprite": "zzz", "brightness": "off"},
      {"name": "lunch", "from": "14:00", "to": "15:00", "sprite": "lunch"},
      {"name": "late", "days": "fri", "from": "23:00", "to": "01:00", "brightness": 5}
    ]}
  ]
}`

// oneChannel is the defaults with the single channel renamed home and given this playlist.
func oneChannel(pictures, clock string) string {
	return `{"location": {"latitude": 41.3874, "longitude": 2.1686}, "brightness": {"day": 80, "dusk": 40, "night": 10},
		"release_for": "3h", "default_channel": "home", "schedule": [],
		"channels": [{"name": "home", "sources": ["*"], "ambient": {"pictures": "` + pictures + `", "clock": "` + clock + `"}, "scenes": []}]}`
}

type memorySaver struct {
	mu    sync.Mutex
	saved []state.State
	fail  error
}

func (m *memorySaver) Save(s state.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.saved = append(m.saved, s)
	return nil
}

// newHub starts from the state in JSON, or the defaults when it is empty.
func newHub(t *testing.T, stateJSON string, ambient ambientDir) (*Hub, *fakeDisplay, *time.Time) {
	t.Helper()
	st := state.Defaults()
	if stateJSON != "" {
		var err error
		if st, err = state.Decode([]byte(stateJSON)); err != nil {
			t.Fatal(err)
		}
	}
	d := &fakeDisplay{}
	h := New(d, st, &memorySaver{}, sprites{"zzz": true, "lunch": true, "heart": true}, ambient, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := friday(12, 0)
	h.now = func() time.Time { return now }
	return h, d, &now
}

func heart(source string) Alert {
	return Alert{Content: display.Content{Kind: display.Picture, Name: "heart"}, Duration: time.Minute, Source: source}
}

func TestLayers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		ambient    ambientDir
		at         time.Time
		alert      bool
		layer      string
		showing    string
		brightness int
	}{
		{"ambient on the home channel", ambientDir{"a.gif"}, friday(19, 0), false, LayerAmbient, "home/a.gif", 10},
		{"empty ambient falls back to the clock", nil, friday(12, 0), false, LayerAmbient, "clock", 80},
		{"ambient at night", nil, friday(20, 0), false, LayerAmbient, "clock", 10},
		{"ambient at dusk", nil, friday(17, 55), false, LayerAmbient, "clock", 40},
		{"scene beats ambient", ambientDir{"a.gif"}, time.Date(2026, 11, 7, 14, 0, 0, 0, madrid), false, LayerScene, "lunch", 80},
		{"scenes belong to their channel", ambientDir{"a.gif"}, friday(14, 0), false, LayerAmbient, "work/a.gif", 80},
		{"scene brightness overrides the sun", nil, friday(6, 0), false, LayerScene, "zzz", 0},
		{"scene without sprite shows the clock", nil, friday(23, 30), false, LayerScene, "clock", 5},
		{"scene past midnight", nil, friday(24, 30), false, LayerScene, "clock", 5},
		{"alert beats scene", nil, friday(6, 30), true, LayerAlert, "heart", 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, d, now := newHub(t, twoChannels, tt.ambient)
			*now = tt.at
			if tt.alert {
				h.Notify(heart(""))
			}
			h.update()
			st := h.Status()
			if st.Layer != tt.layer || st.Showing != tt.showing || st.Brightness != tt.brightness {
				t.Errorf("showing %s %s at %d, want %s %s at %d", st.Layer, st.Showing, st.Brightness, tt.layer, tt.showing, tt.brightness)
			}
			if len(d.shown) != 1 {
				t.Errorf("display got %d contents, want 1", len(d.shown))
			}
		})
	}
}

func TestAlertsExpireByPriority(t *testing.T) {
	t.Parallel()
	h, d, now := newHub(t, "", ambientDir(nil))
	start := *now
	alert := func(name string, priority int, d time.Duration) Alert {
		return Alert{Content: display.Content{Kind: display.Picture, Name: name}, Priority: priority, Duration: d}
	}
	h.Notify(alert("low", 1, 10*time.Second))
	h.Notify(alert("also low", 1, 5*time.Second))
	*now = start.Add(4 * time.Second)
	h.Notify(alert("urgent", 9, 3*time.Second))

	steps := []struct {
		after time.Duration
		want  string
	}{
		{4 * time.Second, "urgent"},
		{6900 * time.Millisecond, "urgent"},
		{7 * time.Second, "low"},
		{12900 * time.Millisecond, "low"},
		{13 * time.Second, "also low"},
		{18 * time.Second, "clock"},
	}
	for _, s := range steps {
		*now = start.Add(s.after)
		h.update()
		if got := h.Status().Showing; got != s.want {
			t.Errorf("after %s showing %s, want %s", s.after, got, s.want)
		}
	}
	var names []string
	for _, c := range d.shown {
		names = append(names, c.Name)
	}
	if want := "urgent low also low clock"; strings.Join(names, " ") != want {
		t.Errorf("display got %q, want %q", strings.Join(names, " "), want)
	}
}

func TestSourceFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		at       time.Time
		source   string
		channel  string
		accepted bool
	}{
		{"work takes work", friday(10, 0), "work", "work", true},
		{"work takes system", friday(10, 0), "system", "work", true},
		{"work drops football", friday(10, 0), "football", "work", false},
		{"work drops alerts without a source", friday(10, 0), "", "work", false},
		{"home takes anything", friday(20, 0), "football", "home", true},
		{"home takes alerts without a source", friday(20, 0), "", "home", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _, now := newHub(t, twoChannels, ambientDir(nil))
			*now = tt.at
			channel, accepted := h.Notify(heart(tt.source))
			if channel != tt.channel || accepted != tt.accepted {
				t.Errorf("Notify = %s %v, want %s %v", channel, accepted, tt.channel, tt.accepted)
			}
			h.update()
			if shown := h.Status().Layer == LayerAlert; shown != tt.accepted {
				t.Errorf("alert on screen %v, want %v", shown, tt.accepted)
			}
		})
	}
}

func TestChannelResolution(t *testing.T) {
	t.Parallel()
	saturday := func(hour int) time.Time { return time.Date(2026, 11, 7, hour, 0, 0, 0, madrid) }
	monday := func(hour int) time.Time { return time.Date(2026, 11, 9, hour, 0, 0, 0, madrid) }
	type check struct {
		at      time.Time
		channel string
		reason  string
	}
	tests := []struct {
		name   string
		pinAt  time.Time
		pin    string
		pinFor time.Duration
		checks []check
	}{
		{"schedule across weekdays", time.Time{}, "", 0, []check{
			{friday(8, 59), "home", ReasonSchedule},
			{friday(9, 0), "work", ReasonSchedule},
			{friday(17, 59), "work", ReasonSchedule},
			{friday(18, 0), "home", ReasonSchedule},
			{saturday(12), "home", ReasonSchedule},
			{monday(10), "work", ReasonSchedule},
		}},
		{"override until the schedule changes", friday(10, 0), "home", 0, []check{
			{friday(17, 59), "home", ReasonOverride},
			{friday(18, 0), "home", ReasonSchedule},
			{friday(19, 0), "home", ReasonSchedule},
		}},
		{"override over a weekend", saturday(12), "work", 0, []check{
			{saturday(23), "work", ReasonOverride},
			{monday(8), "work", ReasonOverride},
			{monday(9), "work", ReasonSchedule},
			{monday(18), "home", ReasonSchedule},
		}},
		{"override for a while outlives the schedule", friday(17, 0), "work", 4 * time.Hour, []check{
			{friday(18, 30), "work", ReasonOverride},
			{friday(20, 59), "work", ReasonOverride},
			{friday(21, 0), "home", ReasonSchedule},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _, now := newHub(t, twoChannels, ambientDir(nil))
			if tt.pin != "" {
				*now = tt.pinAt
				if err := h.SetChannel(tt.pin, tt.pinFor); err != nil {
					t.Fatal(err)
				}
			}
			for _, c := range tt.checks {
				*now = c.at
				st := h.Status()
				if st.Channel != c.channel || st.ChannelReason != c.reason {
					t.Errorf("%s: %s by %s, want %s by %s", c.at.Format("Mon 15:04"), st.Channel, st.ChannelReason, c.channel, c.reason)
				}
			}
		})
	}
}

func TestChannelChangeRepaintsAndClears(t *testing.T) {
	t.Parallel()
	h, d, now := newHub(t, twoChannels, ambientDir{"a.gif"})
	*now = friday(14, 30)
	h.update()
	if err := h.SetChannel("home", 0); err != nil {
		t.Fatal(err)
	}
	h.update()
	h.ClearChannel()
	h.update()
	var names []string
	for _, c := range d.shown {
		names = append(names, c.Name)
	}
	if got, want := strings.Join(names, " "), "work/a.gif lunch work/a.gif"; got != want {
		t.Errorf("display got %q, want %q", got, want)
	}
	if err := h.SetChannel("gym", 0); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("unknown channel: %v", err)
	}
}

func TestAmbientPlaylist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state string
		steps map[time.Duration]string
	}{
		{"pictures then clock", twoChannels, map[time.Duration]string{
			0:                           "work/a.gif",
			time.Minute:                 "work/a.gif",
			2 * time.Minute:             "clock",
			5*time.Minute - time.Second: "clock",
			5 * time.Minute:             "work/b.gif",
			7 * time.Minute:             "clock",
			10 * time.Minute:            "work/a.gif",
		}},
		{"pictures only", oneChannel("5m", "0s"), map[time.Duration]string{
			0:                "home/a.gif",
			4 * time.Minute:  "home/a.gif",
			5 * time.Minute:  "home/b.gif",
			10 * time.Minute: "home/a.gif",
		}},
		{"clock only", oneChannel("0s", "1m"), map[time.Duration]string{
			0:               "clock",
			5 * time.Minute: "clock",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _, now := newHub(t, tt.state, ambientDir{"a.gif", "b.gif"})
			start := friday(10, 0)
			var offsets []time.Duration
			for after := range tt.steps {
				offsets = append(offsets, after)
			}
			slices.Sort(offsets)
			for _, after := range offsets {
				*now = start.Add(after)
				h.update()
				if got := h.Status().Showing; got != tt.steps[after] {
					t.Errorf("after %s showing %s, want %s", after, got, tt.steps[after])
				}
			}
		})
	}
}

func TestReleaseClaimsItselfBack(t *testing.T) {
	t.Parallel()
	h, d, now := newHub(t, "", ambientDir(nil))
	until := h.Release()
	if !d.released || !until.Equal(now.Add(3*time.Hour)) || h.Status().ReleasedUntil == nil {
		t.Fatalf("release until %s, display released %v", until, d.released)
	}
	*now = now.Add(3*time.Hour - time.Second)
	h.update()
	if !d.released {
		t.Fatal("claimed before the release time")
	}
	*now = now.Add(time.Second)
	h.update()
	if d.released || h.Status().ReleasedUntil != nil {
		t.Error("still released after the release time")
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	h, d, now := newHub(t, twoChannels, ambientDir(nil))
	*now = time.Date(2026, 11, 7, 14, 30, 0, 0, madrid)
	h.update()
	if err := h.SetChannel("work", 0); err != nil {
		t.Fatal(err)
	}
	h.update()
	_, before := h.Preview()

	_, err := h.Update(func(s *state.State) error {
		s.Channels[0].Scenes = append(s.Channels[0].Scenes, state.Scene{Name: "focus", Window: state.Window{From: 600, To: 1080}, Sprite: "heart"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.update()
	content, after := h.Preview()
	if content.Name != "heart" || after == before {
		t.Errorf("after adding a scene showing %s (preview %d -> %d), want heart", content.Name, before, after)
	}

	if _, err := h.Update(func(s *state.State) error { return s.DeleteChannel("work") }); err != nil {
		t.Fatal(err)
	}
	if st := h.Status(); st.Channel != "home" || st.ChannelReason != ReasonSchedule {
		t.Errorf("override survived its channel: %s by %s", st.Channel, st.ChannelReason)
	}
	h.update()
	if last := d.shown[len(d.shown)-1]; last.Name != "lunch" {
		t.Errorf("display shows %s after the delete, want lunch", last.Name)
	}
}

func TestUpdateRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*state.State) error
		want   error
	}{
		{"unknown sprite", func(s *state.State) error {
			s.Channels[0].Scenes = []state.Scene{{Name: "x", Window: state.Window{From: 1, To: 2}, Sprite: "nope"}}
			return nil
		}, state.ErrInvalid},
		{"invalid state", func(s *state.State) error { s.DefaultChannel = "gym"; return nil }, state.ErrInvalid},
		{"refused change", func(s *state.State) error { return s.DeleteChannel("home") }, state.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := newHub(t, twoChannels, ambientDir(nil))
			before := h.State()
			if _, err := h.Update(tt.change); !errors.Is(err, tt.want) {
				t.Errorf("error %v, want %v", err, tt.want)
			}
			if after := h.State(); len(after.Channels) != len(before.Channels) || after.DefaultChannel != before.DefaultChannel ||
				len(after.Channels[0].Scenes) != len(before.Channels[0].Scenes) {
				t.Error("a rejected change reached the live state")
			}
			if saved := h.store.(*memorySaver).saved; len(saved) != 0 {
				t.Errorf("a rejected change was saved")
			}
		})
	}
}

func TestSaveFailureKeepsTheOldState(t *testing.T) {
	t.Parallel()
	h, _, _ := newHub(t, twoChannels, ambientDir(nil))
	h.store.(*memorySaver).fail = errors.New("disk full")
	if _, err := h.Update(func(s *state.State) error { s.DefaultChannel = "work"; return nil }); err == nil {
		t.Fatal("want the save error")
	}
	if h.State().DefaultChannel != "home" {
		t.Error("an unsaved change went live")
	}
}

func TestMissingSceneSpriteFallsBackToTheClock(t *testing.T) {
	t.Parallel()
	st, err := state.Decode([]byte(strings.Replace(twoChannels, `"sprite": "lunch"`, `"sprite": "gone"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	h := New(&fakeDisplay{}, st, &memorySaver{}, sprites{"zzz": true}, ambientDir(nil), slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, 11, 7, 14, 30, 0, 0, madrid)
	h.now = func() time.Time { return now }
	h.update()
	if s := h.Status(); s.Layer != LayerScene || s.Showing != "clock" {
		t.Errorf("showing %s %s, want the lunch scene as the clock", s.Layer, s.Showing)
	}
}

func TestReplaceKeepsTheOverride(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, newName, want string }{
		{"rename", "studio", "studio"},
		{"same name", "work", "work"},
		{"name left out", "", "work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _, now := newHub(t, twoChannels, ambientDir(nil))
			*now = time.Date(2026, 11, 7, 12, 0, 0, 0, madrid)
			if err := h.SetChannel("work", 0); err != nil {
				t.Fatal(err)
			}
			c, _ := h.State().Channel("work")
			c.Name = tt.newName
			if _, err := h.ReplaceChannel("work", c); err != nil {
				t.Fatal(err)
			}
			if st := h.Status(); st.Channel != tt.want || st.ChannelReason != ReasonOverride {
				t.Errorf("after the update: %s by %s, want %s by override", st.Channel, st.ChannelReason, tt.want)
			}
		})
	}
}

func TestAMissingSpriteDoesNotBlockOtherChanges(t *testing.T) {
	t.Parallel()
	st, err := state.Decode([]byte(strings.Replace(twoChannels, `"sprite": "lunch"`, `"sprite": "gone"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	h := New(&fakeDisplay{}, st, &memorySaver{}, sprites{"zzz": true, "heart": true}, ambientDir(nil), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := h.Update(func(s *state.State) error { s.Brightness.Day = 50; return nil }); err != nil {
		t.Errorf("settings change refused over an old missing sprite: %v", err)
	}
	_, err = h.Update(func(s *state.State) error {
		s.Channels[0].Scenes = []state.Scene{{Name: "x", Window: state.Window{From: 1, To: 2}, Sprite: "also-gone"}}
		return nil
	})
	if !errors.Is(err, state.ErrInvalid) {
		t.Errorf("a new missing sprite: %v", err)
	}
}

func TestPicturesNeedTheirChannel(t *testing.T) {
	t.Parallel()
	h, _, _ := newHub(t, twoChannels, ambientDir(nil))
	if _, err := h.AddPicture("gym", "a.png", nil); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("add to a missing channel: %v", err)
	}
	if err := h.RemovePicture("gym", "a.png"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("remove from a missing channel: %v", err)
	}
	if _, err := h.ListPictures("gym"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("list a missing channel: %v", err)
	}
}
