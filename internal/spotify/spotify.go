// Package spotify links one Spotify account and follows what it plays, on any of its devices.
package spotify

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

const (
	scopes       = "user-read-playback-state user-read-currently-playing"
	pollEvery    = 5 * time.Second
	pendingFor   = 10 * time.Minute
	maxCoverSize = 1 << 20
	coverSize    = 16
)

var (
	ErrNotConfigured = errors.New("no Spotify client ID yet")
	ErrUnknownLogin  = errors.New("this sign-in was not started here or took too long; start it again")
)

// Playing is what the account plays now, on whichever device.
type Playing struct {
	Playing bool        `json:"playing"`
	Track   string      `json:"track"`
	Artists string      `json:"artists"`
	Album   string      `json:"album"`
	Device  string      `json:"device"`
	Cover   image.Image `json:"-"`
	coverAt string
}

type Status struct {
	Configured bool     `json:"configured"`
	ClientID   string   `json:"client_id,omitempty"`
	Connected  bool     `json:"connected"`
	User       string   `json:"user,omitempty"`
	Now        *Playing `json:"now,omitempty"`
	// Devices are the account's devices seen so far, to pick which ones show on the display.
	Devices []string `json:"devices"`
	Error   string   `json:"error,omitempty"`
}

type saved struct {
	ClientID string `json:"client_id"`
	Refresh  string `json:"refresh_token,omitempty"`
	User     string `json:"user,omitempty"`
}

type login struct {
	verifier, redirect string
	at                 time.Time
}

type Account struct {
	path string
	http *http.Client
	log  *slog.Logger
	// AccountsURL and APIURL are Spotify's, replaced in tests.
	AccountsURL, APIURL string

	mu      sync.Mutex
	saved   saved
	access  string
	expires time.Time
	pending map[string]login
	now     Playing
	lastErr string
	devices map[string]bool
	// previous and flipAt are the cover before the last change and when it changed, for the flip.
	previous image.Image
	flipAt   time.Time
	// anim keeps the frames last made for a cover, since the hub asks every second.
	anim struct {
		cover  image.Image
		key    string
		frames []display.Frame
	}
	listed time.Time
	wake   chan struct{}
}

// Open reads the saved link from path; a missing file is an account not set up yet.
func Open(path string, log *slog.Logger) (*Account, error) {
	a := &Account{
		path: path, http: &http.Client{Timeout: 15 * time.Second}, log: log,
		AccountsURL: "https://accounts.spotify.com", APIURL: "https://api.spotify.com/v1",
		pending: map[string]login{}, wake: make(chan struct{}, 1),
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

func (a *Account) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Status{Configured: a.saved.ClientID != "", ClientID: a.saved.ClientID, Connected: a.saved.Refresh != "", User: a.saved.User, Error: a.lastErr,
		Devices: slices.Sorted(maps.Keys(a.devices))}
	if a.now.Track != "" {
		now := a.now
		s.Now = &now
	}
	return s
}

// Now is the track being played, if any.
func (a *Account) Now() (Playing, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.now, a.now.Playing
}

// SetClientID links a Spotify app; a different one forgets the account signed in with the old.
func (a *Account) SetClientID(id string) error {
	id = strings.TrimSpace(id)
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return fmt.Errorf("client ID %q: want the 32 hex characters from the Spotify dashboard", id)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if id == a.saved.ClientID {
		return nil
	}
	return a.saveLocked(saved{ClientID: id})
}

func (a *Account) Forget() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.access, a.now, a.lastErr = "", Playing{}, ""
	return a.saveLocked(saved{ClientID: a.saved.ClientID})
}

// LoginURL starts a sign-in that comes back to redirect, which must be listed in the Spotify app.
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
		"client_id": {a.saved.ClientID}, "response_type": {"code"}, "redirect_uri": {redirect}, "scope": {scopes},
		"state": {state}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
	}
	return a.AccountsURL + "/authorize?" + q.Encode(), nil
}

// Finish trades the code Spotify sent back for a refresh token and keeps it.
func (a *Account) Finish(ctx context.Context, state, code string) error {
	a.mu.Lock()
	l, ok := a.pending[state]
	delete(a.pending, state)
	clientID := a.saved.ClientID
	a.mu.Unlock()
	if !ok || time.Since(l.at) > pendingFor {
		return ErrUnknownLogin
	}
	tok, err := a.token(ctx, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {l.redirect},
		"client_id": {clientID}, "code_verifier": {l.verifier},
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.access, a.expires = tok.Access, time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	err = a.saveLocked(saved{ClientID: clientID, Refresh: tok.Refresh})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	var me struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	}
	if err := a.get(ctx, "/me", &me); err == nil {
		a.mu.Lock()
		next := a.saved
		next.User = cmp.Or(me.DisplayName, me.ID)
		err = a.saveLocked(next)
		a.mu.Unlock()
	}
	a.poke()
	return err
}

// Run follows what the account plays while it is linked.
func (a *Account) Run(ctx context.Context) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		wait := a.poll(ctx)
		if wait > pollEvery {
			t.Reset(wait)
		} else {
			t.Reset(pollEvery)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.wake:
		}
	}
}

func (a *Account) poke() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// poll reads the playback once; it returns how long Spotify asked to wait, if it did.
func (a *Account) poll(ctx context.Context) time.Duration {
	a.mu.Lock()
	linked := a.saved.Refresh != ""
	a.mu.Unlock()
	if !linked {
		return 0
	}
	var p struct {
		IsPlaying bool `json:"is_playing"`
		Device    struct {
			Name string `json:"name"`
		} `json:"device"`
		Item *struct {
			Name    string `json:"name"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album struct {
				Name   string `json:"name"`
				Images []struct {
					URL   string `json:"url"`
					Width int    `json:"width"`
				} `json:"images"`
			} `json:"album"`
		} `json:"item"`
	}
	a.listDevices(ctx)
	err := a.get(ctx, "/me/player", &p)
	var limited *rateLimited
	if errors.As(err, &limited) {
		return limited.wait
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		if ctx.Err() == nil && a.lastErr != err.Error() {
			a.log.Warn("spotify", "err", err)
		}
		a.lastErr = err.Error()
		return 0
	}
	a.lastErr = ""
	if p.Item == nil {
		a.now = Playing{}
		return 0
	}
	var artists []string
	for _, ar := range p.Item.Artists {
		artists = append(artists, ar.Name)
	}
	next := Playing{Playing: p.IsPlaying, Track: p.Item.Name, Artists: strings.Join(artists, ", "), Album: p.Item.Album.Name, Device: p.Device.Name}
	smallest := ""
	for i, im := range p.Item.Album.Images {
		if i == 0 || im.Width < 300 {
			smallest = im.URL
		}
	}
	if smallest == a.now.coverAt {
		next.Cover, next.coverAt = a.now.Cover, a.now.coverAt
	} else if smallest != "" {
		a.mu.Unlock()
		cover, err := a.cover(ctx, smallest)
		a.mu.Lock()
		if err != nil {
			a.log.Warn("spotify cover", "err", err)
		} else {
			next.Cover, next.coverAt = cover, smallest
			a.previous, a.flipAt = a.now.Cover, time.Now()
		}
	}
	a.seen(next.Device)
	if next.Track != a.now.Track || next.Playing != a.now.Playing {
		a.log.Info("spotify", "playing", next.Playing, "track", next.Track, "artists", next.Artists, "device", next.Device)
	}
	a.now = next
	return 0
}

// listDevices asks for the account's devices at most once a minute, so they can be picked before
// they ever play.
func (a *Account) listDevices(ctx context.Context) {
	a.mu.Lock()
	due := time.Since(a.listed) > time.Minute
	if due {
		a.listed = time.Now()
	}
	a.mu.Unlock()
	if !due {
		return
	}
	var d struct {
		Devices []struct {
			Name string `json:"name"`
		} `json:"devices"`
	}
	if err := a.get(ctx, "/me/player/devices", &d); err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, dev := range d.Devices {
		a.seen(dev.Name)
	}
}

// seen needs a.mu held.
func (a *Account) seen(device string) {
	if device == "" {
		return
	}
	if a.devices == nil {
		a.devices = map[string]bool{}
	}
	a.devices[device] = true
}

func (a *Account) cover(ctx context.Context, u string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover: HTTP %d", res.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(res.Body, maxCoverSize))
	if err != nil {
		return nil, err
	}
	return forLEDs(shrink(img, coverSize)), nil
}

// forLEDs makes a shrunk photo read on a LED grid, where mid tones glow almost as bright as
// highlights: it stretches the levels, adds saturation, then darkens mid tones with a gamma.
func forLEDs(img *image.NRGBA) *image.NRGBA {
	const saturation, gamma = 1.5, 2.2
	lo, hi := 255.0, 0.0
	for i := 0; i < len(img.Pix); i += 4 {
		l := luma(img.Pix[i : i+3])
		lo, hi = min(lo, l), max(hi, l)
	}
	span := max(hi-lo, 1)
	for i := 0; i < len(img.Pix); i += 4 {
		px := img.Pix[i : i+3]
		grey := luma(px)
		for c := range px {
			v := grey + (float64(px[c])-grey)*saturation
			v = (v - lo) / span
			px[c] = uint8(math.Round(255 * math.Pow(min(max(v, 0), 1), gamma)))
		}
	}
	return img
}

func luma(px []uint8) float64 {
	return 0.299*float64(px[0]) + 0.587*float64(px[1]) + 0.114*float64(px[2])
}

// shrink averages each block of pixels, which keeps a photo readable at 16x16 where picking one
// pixel per block would not.
func shrink(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			x0, x1 := b.Min.X+x*b.Dx()/size, b.Min.X+(x+1)*b.Dx()/size
			y0, y1 := b.Min.Y+y*b.Dy()/size, b.Min.Y+(y+1)*b.Dy()/size
			var r, g, bl, n uint64
			for yy := y0; yy < max(y1, y0+1); yy++ {
				for xx := x0; xx < max(x1, x0+1); xx++ {
					cr, cg, cb, _ := src.At(xx, yy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			dst.Set(x, y, color.NRGBA{R: uint8(r / n >> 8), G: uint8(g / n >> 8), B: uint8(bl / n >> 8), A: 255})
		}
	}
	return dst
}

// Content is the cover of the current track and its device; playing is false while paused. The
// cover turns half way round now and then, and from the old one for a moment after it changes.
func (a *Account) Content() (display.Content, string, bool) {
	p, playing := a.Now()
	if p.Cover == nil {
		return display.Content{}, p.Device, false
	}
	// Named after the album, which owns the cover, so the next track of it sends nothing new.
	c := display.Content{Kind: display.Picture, Name: "♪ " + cmp.Or(p.Album, p.Track), Key: "idle"}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.previous != nil && time.Since(a.flipAt) < flipFor {
		c.Key = "flip"
	}
	if a.anim.cover != p.Cover || a.anim.key != c.Key {
		a.anim.cover, a.anim.key = p.Cover, c.Key
		if c.Key == "flip" {
			a.anim.frames = reduce(flip(a.previous, p.Cover))
		} else {
			a.anim.frames = reduce(idle(p.Cover))
		}
	}
	c.Frames = a.anim.frames
	return c, p.Device, playing
}

type rateLimited struct{ wait time.Duration }

func (r *rateLimited) Error() string { return fmt.Sprintf("rate limited for %s", r.wait) }

// get calls the Web API; 204 No Content leaves v as it is.
func (a *Account) get(ctx context.Context, path string, v any) error {
	access, err := a.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.APIURL+path, nil)
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
	case res.StatusCode == http.StatusNoContent:
		return nil
	case res.StatusCode == http.StatusTooManyRequests:
		secs, _ := time.ParseDuration(res.Header.Get("Retry-After") + "s")
		return &rateLimited{wait: max(secs, pollEvery)}
	case res.StatusCode == http.StatusUnauthorized:
		a.mu.Lock()
		a.access = ""
		a.mu.Unlock()
		return fmt.Errorf("%s: HTTP 401", path)
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("%s: HTTP %d", path, res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

func (a *Account) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.access != "" && time.Until(a.expires) > time.Minute {
		defer a.mu.Unlock()
		return a.access, nil
	}
	refresh, clientID := a.saved.Refresh, a.saved.ClientID
	a.mu.Unlock()
	if refresh == "" {
		return "", errors.New("not signed in")
	}
	tok, err := a.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}})
	var grant *grantError
	if errors.As(err, &grant) {
		// The refresh token expired or was revoked: drop it so the page offers to connect again.
		a.mu.Lock()
		defer a.mu.Unlock()
		a.log.Warn("spotify sign-in no longer valid, connect again", "err", err)
		if saveErr := a.saveLocked(saved{ClientID: a.saved.ClientID}); saveErr != nil {
			return "", saveErr
		}
		return "", err
	}
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.access, a.expires = tok.Access, time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	if tok.Refresh != "" && tok.Refresh != a.saved.Refresh {
		next := a.saved
		next.Refresh = tok.Refresh
		if err := a.saveLocked(next); err != nil {
			return "", err
		}
	}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.AccountsURL+"/api/token", strings.NewReader(form.Encode()))
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

func (a *Account) saveLocked(s saved) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(a.path, data); err != nil {
		return err
	}
	a.saved = s
	if s.Refresh == "" {
		a.access, a.now = "", Playing{}
	}
	return nil
}

func random(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
