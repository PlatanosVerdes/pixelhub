package spotify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const clientID = "0123456789abcdef0123456789abcdef"

// fakeSpotify checks the PKCE exchange and serves one track with a red cover.
type fakeSpotify struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	challenge string
	refreshes int
	limited   bool
	revoked   bool
	playing   bool
}

func newFake(t *testing.T) *fakeSpotify {
	f := &fakeSpotify{t: t, playing: true}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/token", f.token)
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"display_name": "Someone", "id": "someone"})
	})
	mux.HandleFunc("GET /v1/me/player", f.player)
	mux.HandleFunc("GET /v1/me/player/devices", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"devices":[{"name":"Phone"}]}`)
	})
	mux.HandleFunc("GET /cover.png", func(w http.ResponseWriter, r *http.Request) {
		img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for i := range img.Pix {
			img.Pix[i] = []uint8{200, 10, 20, 255}[i%4]
		}
		_ = png.Encode(w, img)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSpotify) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.Form.Get("client_id") != clientID {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge || r.Form.Get("code") != "the-code" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"bad verifier"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"a1","refresh_token":"r1","expires_in":3600}`)
	case "refresh_token":
		if f.revoked {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Refresh token revoked"}`)
			return
		}
		f.refreshes++
		_, _ = io.WriteString(w, `{"access_token":"a2","refresh_token":"r2","expires_in":3600}`)
	}
}

func (f *fakeSpotify) player(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.limited {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer a") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"is_playing": f.playing,
		"device":     map[string]string{"name": "Kitchen"},
		"item": map[string]any{
			"name": "Song", "artists": []map[string]string{{"name": "A"}, {"name": "B"}},
			"album": map[string]any{"name": "Album", "images": []map[string]any{
				{"url": f.srv.URL + "/big.png", "width": 640}, {"url": f.srv.URL + "/cover.png", "width": 64},
			}},
		},
	})
}

func open(t *testing.T, f *fakeSpotify) *Account {
	t.Helper()
	a, err := Open(filepath.Join(t.TempDir(), "spotify.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	a.AccountsURL, a.APIURL = f.srv.URL, f.srv.URL+"/v1"
	return a
}

func signIn(t *testing.T, a *Account, f *fakeSpotify) {
	t.Helper()
	if err := a.SetClientID(clientID); err != nil {
		t.Fatal(err)
	}
	to, err := a.LoginURL("http://127.0.0.1:8765/api/spotify/callback")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(to)
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "http://127.0.0.1:8765/api/spotify/callback" || !strings.Contains(q.Get("scope"), "user-read-playback-state") {
		t.Fatalf("authorize URL %s", to)
	}
	f.mu.Lock()
	f.challenge = q.Get("code_challenge")
	f.mu.Unlock()
	if err := a.Finish(context.Background(), q.Get("state"), "the-code"); err != nil {
		t.Fatal(err)
	}
}

func TestSignInFollowsPlayback(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	a := open(t, f)
	signIn(t, a, f)
	if s := a.Status(); !s.Connected || s.User != "Someone" {
		t.Fatalf("status %+v", s)
	}
	a.poll(context.Background())
	p, ok := a.Now()
	if !ok || p.Track != "Song" || p.Artists != "A, B" || p.Device != "Kitchen" {
		t.Fatalf("now %+v, %v", p, ok)
	}
	c, device, ok := a.Content()
	if !ok || c.Name != "♪ Album" || device != "Kitchen" || c.Frames[0].Image.Bounds().Dx() != coverSize {
		t.Fatalf("content %+v on %q, %v", c, device, ok)
	}
	if s := a.Status(); !slices.Equal(s.Devices, []string{"Kitchen", "Phone"}) {
		t.Errorf("devices %v, want the listed and the playing ones", s.Devices)
	}
	if c.Key != "idle" || len(c.Frames) != 1 {
		t.Errorf("the first cover shows %d frames with key %q, want it still", len(c.Frames), c.Key)
	}
	cover := c.Frames[0].Image
	if got := color.NRGBAModel.Convert(cover.At(3, 3)).(color.NRGBA); got.R < 200 || got.G > 10 || got.B > 10 {
		t.Errorf("cover pixel %v, want the cover's red, made purer", got)
	}
	a.mu.Lock()
	a.previous, a.flipAt = image.NewNRGBA(image.Rect(0, 0, coverSize, coverSize)), time.Now()
	a.mu.Unlock()
	if c, _, _ := a.Content(); c.Key != "flip" || len(c.Frames) < 2 || !sameImage(c.Frames[len(c.Frames)-1].Image, cover) {
		t.Errorf("a changed cover shows %d frames with key %q, want the half turn ending on it", len(c.Frames), c.Key)
	}
	a.mu.Lock()
	a.flipAt = time.Now().Add(-flipFor)
	a.mu.Unlock()
	if c, _, _ := a.Content(); c.Key != "idle" || !sameImage(c.Frames[0].Image, cover) {
		t.Errorf("after the half turn: key %q, want the cover idling again", c.Key)
	}
}

func TestPausedShowsNothing(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	f.playing = false
	a := open(t, f)
	signIn(t, a, f)
	a.poll(context.Background())
	if _, _, ok := a.Content(); ok {
		t.Error("a paused track should not show")
	}
	if s := a.Status(); s.Now == nil || s.Now.Playing {
		t.Errorf("status should still name the paused track: %+v", s.Now)
	}
}

func TestLinkSurvivesARestart(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	a := open(t, f)
	signIn(t, a, f)
	b, err := Open(a.path, a.log)
	if err != nil {
		t.Fatal(err)
	}
	b.AccountsURL, b.APIURL = f.srv.URL, f.srv.URL+"/v1"
	b.poll(context.Background())
	if _, ok := b.Now(); !ok || f.refreshes != 1 {
		t.Fatalf("after a restart: playing %v, %d refreshes", ok, f.refreshes)
	}
	data, _ := os.ReadFile(a.path)
	if !bytes.Contains(data, []byte(`"refresh_token": "r2"`)) {
		t.Errorf("the rotated refresh token was not kept: %s", data)
	}
	if info, _ := os.Stat(a.path); info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v, want 0600", info.Mode().Perm())
	}
}

func TestRejects(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	a := open(t, f)
	if _, err := a.LoginURL("http://x/cb"); err != ErrNotConfigured {
		t.Errorf("login without a client ID: %v", err)
	}
	if err := a.SetClientID("not-an-id"); err == nil {
		t.Error("want an error for a malformed client ID")
	}
	if err := a.SetClientID(clientID); err != nil {
		t.Fatal(err)
	}
	if err := a.Finish(context.Background(), "made-up", "the-code"); err != ErrUnknownLogin {
		t.Errorf("unknown state: %v", err)
	}
	to, _ := a.LoginURL("http://x/cb")
	u, _ := url.Parse(to)
	if err := a.Finish(context.Background(), u.Query().Get("state"), "the-code"); err == nil || !strings.Contains(err.Error(), "bad verifier") {
		t.Errorf("a verifier the server did not see: %v", err)
	}
}

func TestRateLimitWaits(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	a := open(t, f)
	signIn(t, a, f)
	f.mu.Lock()
	f.limited = true
	f.mu.Unlock()
	if wait := a.poll(context.Background()); wait != 30*time.Second {
		t.Errorf("waited %s, want the 30s Spotify asked for", wait)
	}
}

func TestForgetKeepsTheApp(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	a := open(t, f)
	signIn(t, a, f)
	a.poll(context.Background())
	if err := a.Forget(); err != nil {
		t.Fatal(err)
	}
	s := a.Status()
	if s.Connected || !s.Configured || s.Now != nil {
		t.Errorf("after forget: %+v", s)
	}
}

func TestRevokedSignInDisconnects(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	a := open(t, f)
	signIn(t, a, f)
	a.mu.Lock()
	a.access = ""
	a.mu.Unlock()
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	a.poll(context.Background())
	if s := a.Status(); s.Connected || !s.Configured {
		t.Errorf("after a revoked refresh token: %+v", s)
	}
}

func TestForLEDsSpreadsTheLevels(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	copy(img.Pix, []uint8{90, 60, 110, 255, 200, 150, 220, 255})
	forLEDs(img)
	dark, light := img.Pix[0:3], img.Pix[4:7]
	if luma(dark) > 20 {
		t.Errorf("the darkest pixel should go dark and keep its hue: %v", dark)
	}
	if light[0] < 200 || light[2] < 200 {
		t.Errorf("the lightest pixel should stay bright: %v", light)
	}
}

func sameImage(a, b image.Image) bool {
	for y := range coverSize {
		for x := range coverSize {
			if color.NRGBAModel.Convert(a.At(x, y)) != color.NRGBAModel.Convert(b.At(x, y)) {
				return false
			}
		}
	}
	return true
}

func TestAnimationsShareAFewColours(t *testing.T) {
	t.Parallel()
	cover := image.NewNRGBA(image.Rect(0, 0, coverSize, coverSize))
	for i := range cover.Pix {
		cover.Pix[i] = uint8(i * 37)
		if i%4 == 3 {
			cover.Pix[i] = 255
		}
	}
	all := map[color.NRGBA]bool{}
	for _, f := range reduce(idle(cover)) {
		b := f.Image.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				all[color.NRGBAModel.Convert(f.Image.At(x, y)).(color.NRGBA)] = true
			}
		}
	}
	if len(all) > animColors {
		t.Errorf("%d colours across the frames, want at most %d", len(all), animColors)
	}
}
