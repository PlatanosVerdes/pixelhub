package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/favorites"
	"github.com/PlatanosVerdes/pixelhub/internal/gallery"
	"github.com/PlatanosVerdes/pixelhub/internal/hub"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
)

type nullDisplay struct{}

func (nullDisplay) Show(display.Content)   {}
func (nullDisplay) Release()               {}
func (nullDisplay) Claim()                 {}
func (nullDisplay) Status() display.Status { return display.Status{Address: "192.0.2.10:7777"} }

type memorySaver struct{}

func (memorySaver) Save(state.State) error { return nil }

const testState = `{
  "location": {"latitude": 10, "longitude": 20},
  "brightness": {"day": 80, "dusk": 40, "night": 10},
  "release_for": "3h",
  "default_channel": "evening",
  "schedule": [{"channel": "office", "from": "00:00", "to": "24:00"}],
  "channels": [
    {"name": "office", "sources": ["office"], "ambient": {"pictures": "2m", "clock": "3m"}, "scenes": []},
    {"name": "evening", "sources": ["*"], "ambient": {"pictures": "0s", "clock": "1m"}, "scenes": []}
  ]
}`

type fixture struct {
	srv      http.Handler
	hub      *hub.Hub
	pictures sprite.Ambient
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	st, err := state.Decode([]byte(testState))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sprites, err := sprite.Load("", 16)
	if err != nil {
		t.Fatal(err)
	}
	pictures := sprite.Ambient{Dir: t.TempDir(), Size: 16}
	h := hub.New(nullDisplay{}, st, memorySaver{}, sprites, pictures, log)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	for _, v := h.Preview(); v == 0; _, v = h.Preview() {
		time.Sleep(5 * time.Millisecond)
	}
	srv := (&Server{Hub: h, Sprites: sprites, Size: 16, Version: "test"}).Handler()
	return fixture{srv: srv, hub: h, pictures: pictures}
}

func (f fixture) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestNotify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		code     int
		contains string
	}{
		{"sprite from an accepted source", `{"sprite":"heart","source":"office"}`, 202, `"showing":"heart"`},
		{"color with priority and duration", `{"color":[255,0,16],"priority":7,"duration":"3s","source":"office"}`, 202, `"showing":"color #ff0010"`},
		{"source the channel drops", `{"sprite":"heart","source":"football"}`, 200, `"dropped":true`},
		{"no source on a channel without *", `{"sprite":"heart"}`, 200, `"dropped":true`},
		{"unknown sprite", `{"sprite":"dragon"}`, 400, "no sprite"},
		{"both", `{"sprite":"heart","color":[1,2,3]}`, 400, "not both"},
		{"neither", `{"priority":3}`, 400, "sprite or a color"},
		{"short color", `{"color":[1,2]}`, 400, "[r, g, b]"},
		{"color out of range", `{"color":[1,2,300]}`, 400, "0-255"},
		{"priority out of range", `{"sprite":"heart","priority":11}`, 400, "priority"},
		{"bad duration", `{"sprite":"heart","duration":"soon"}`, 400, "duration"},
		{"too long", `{"sprite":"heart","duration":"2h"}`, 400, "duration"},
		{"unknown field", `{"sprite":"heart","text":"hi"}`, 400, "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := newFixture(t).do(t, "POST", "/api/notify", tt.body)
			if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.contains) {
				t.Errorf("code %d body %s, want %d with %s", rec.Code, rec.Body, tt.code, tt.contains)
			}
		})
	}
}

func TestEndpoints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method, path, body string
		code               int
		contains           string
	}{
		{"GET", "/healthz", "", 200, `"status":"ok"`},
		{"GET", "/api/status", "", 200, `"address":"192.0.2.10:7777"`},
		{"GET", "/api/status", "", 200, `"preview_version"`},
		{"GET", "/api/sprites", "", 200, `"heart"`},
		{"POST", "/api/release", "", 200, `"released_until"`},
		{"POST", "/api/claim", "", 200, `"claimed":true`},
		{"GET", "/api/release", "", 405, ""},
		{"GET", "/api/state", "", 200, `"default_channel":"evening"`},
		{"GET", "/api/channels", "", 200, `"name":"office"`},
		{"POST", "/api/channel", `{"name":"evening","for":"4h"}`, 200, `"channel_reason":"override"`},
		{"POST", "/api/channel", `{"name":"evening"}`, 200, `"channel":"evening"`},
		{"POST", "/api/channel", `{"name":"gym"}`, 404, "no channel"},
		{"POST", "/api/channel", `{"name":"office","for":"-1h"}`, 400, "for"},
		{"DELETE", "/api/channel", "", 200, `"channel_reason":"schedule"`},
		{"PUT", "/api/settings", `{"brightness":{"day":50,"dusk":20,"night":0}}`, 200, `"day":50`},
		{"PUT", "/api/settings", `{"release_for":"30m"}`, 200, `"release_for":"30m0s"`},
		{"PUT", "/api/settings", `{"brightness":{"night":3}}`, 200, `"brightness":{"day":80,"dusk":40,"night":3}`},
		{"PUT", "/api/settings", `{"volume":3}`, 400, "unknown field"},
		{"PUT", "/api/settings", `{"release_for":"10s"}`, 400, "release_for"},
		{"PUT", "/api/settings", `{"default_channel":"gym"}`, 400, "default_channel"},
		{"PUT", "/api/settings", `{"location":{"latitude":95,"longitude":0}}`, 400, "location"},
		{"PUT", "/api/schedule", `[{"channel":"evening","days":"sat,sun","from":"10:00","to":"12:00"}]`, 200, `"days":"sat,sun"`},
		{"PUT", "/api/schedule", `[{"channel":"gym","from":"10:00","to":"12:00"}]`, 400, "schedule[0]"},
		{"PUT", "/api/schedule", `[{"channel":"evening","from":"25:00","to":"12:00"}]`, 400, "not a time"},
		{"GET", "/api/modes", "", 200, `"visualizer":12`},
		{"PUT", "/api/mode", `{"mode":"visualizer","style":4}`, 200, `"mode":{"mode":"visualizer","style":4}`},
		{"PUT", "/api/mode", `{"mode":"clock","style":1,"weather":true,"color":"#00ff00"}`, 200, `"color":"#00ff00"`},
		{"PUT", "/api/mode", `{"mode":"scoreboard","red":3,"blue":5}`, 200, `"red":3`},
		{"PUT", "/api/mode", `{"mode":"vj","style":1}`, 400, "mode"},
		{"PUT", "/api/mode", `{"mode":"visualizer","style":12}`, 400, "outside 0-11"},
		{"PUT", "/api/mode", `{"mode":"light","color":"red"}`, 400, "#rrggbb"},
		{"PUT", "/api/mode", `{"mode":"animations","list":1}`, 400, "unknown field"},
		{"DELETE", "/api/mode", "", 200, `"default_channel"`},
		{"PUT", "/api/settings", `{"music":{"enabled":true}}`, 200, `"music":{"enabled":true}`},
		{"PUT", "/api/settings", `{"music":{"enabled":true,"view":{"mode":"visualizer","style":40}}}`, 400, "music.view"},
		{"GET", "/api/spotify", "", 404, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path+" "+tt.body, func(t *testing.T) {
			t.Parallel()
			rec := newFixture(t).do(t, tt.method, tt.path, tt.body)
			if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.contains) {
				t.Errorf("code %d body %s, want %d with %s", rec.Code, rec.Body, tt.code, tt.contains)
			}
		})
	}
}

func TestMusicSettingsReplaceTheView(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.do(t, "PUT", "/api/settings", `{"music":{"enabled":true,"view":{"mode":"visualizer"}}}`)
	rec := f.do(t, "PUT", "/api/settings", `{"music":{"enabled":true}}`)
	if strings.Contains(rec.Body.String(), "visualizer") {
		t.Errorf("going back to the cover kept the view: %s", rec.Body)
	}
}

func TestCallbackURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host, proto, want string
	}{
		{"localhost:8765", "", "http://127.0.0.1:8765/api/spotify/callback"},
		{"127.0.0.1:8765", "", "http://127.0.0.1:8765/api/spotify/callback"},
		{"pixelhub.example.com", "https", "https://pixelhub.example.com/api/spotify/callback"},
		{"pixelhub.pi", "", "http://pixelhub.pi/api/spotify/callback"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/api/spotify/login", nil)
		r.Host = tt.host
		if tt.proto != "" {
			r.Header.Set("X-Forwarded-Proto", tt.proto)
		}
		if got := callbackURL(r); got != tt.want {
			t.Errorf("%s %s: %s, want %s", tt.proto, tt.host, got, tt.want)
		}
	}
}

func TestChannelCRUD(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	steps := []struct {
		method, path, body string
		code               int
		contains           string
	}{
		{"POST", "/api/channels", `{"name":"garden","sources":["*"]}`, 200, `"name":"garden"`},
		{"POST", "/api/channels", `{"name":"garden","sources":["*"]}`, 409, "already exists"},
		{"POST", "/api/channels", `{"name":"Bad Name"}`, 400, "channel name"},
		{"POST", "/api/channels", `{"name":"lit","scenes":[{"name":"x","from":"01:00","to":"02:00","sprite":"dragon"}]}`, 400, "dragon"},
		{"PUT", "/api/channels/office", `{"name":"studio","sources":["office"],"ambient":{"pictures":"1m","clock":"1m"}}`, 200, `"channel":"studio"`},
		{"PUT", "/api/channels/office", `{"sources":["*"],"ambient":{"pictures":"1m","clock":"1m"}}`, 404, "no channel"},
		{"PUT", "/api/channels/studio", `{"name":"evening","ambient":{"pictures":"1m","clock":"1m"}}`, 409, "already exists"},
		{"DELETE", "/api/channels/evening", "", 409, "default channel"},
		{"DELETE", "/api/channels/gym", "", 404, "no channel"},
		{"DELETE", "/api/channels/studio", "", 200, `"schedule":[]`},
	}
	if _, err := f.pictures.Add("office", "cat.png", pngBytes(t)); err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		rec := f.do(t, s.method, s.path, s.body)
		if rec.Code != s.code || !strings.Contains(rec.Body.String(), s.contains) {
			t.Fatalf("%s %s: code %d body %s, want %d with %s", s.method, s.path, rec.Code, rec.Body, s.code, s.contains)
		}
		if s.path == "/api/channels/office" && s.code == 200 {
			if files, _ := f.pictures.List("studio"); len(files) != 1 {
				t.Errorf("pictures did not follow the rename: %v", files)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(f.pictures.Dir, "studio")); !os.IsNotExist(err) {
		t.Errorf("the deleted channel's pictures are still there: %v", err)
	}
	var channels []state.Channel
	if err := json.Unmarshal(f.do(t, "GET", "/api/channels", "").Body.Bytes(), &channels); err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 || channels[0].Name != "evening" || channels[1].Name != "garden" || channels[1].Ambient != state.NewAmbient() {
		t.Errorf("channels %+v", channels)
	}
}

func TestPictures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		channel  string
		filename string
		data     []byte
		code     int
		contains string
	}{
		{"png", "office", "cat.png", pngBytes(t), 201, `"file":"cat.png"`},
		{"traversal name is cleaned", "office", "../../cat.png", pngBytes(t), 201, `"file":"cat.png"`},
		{"not a picture", "office", "cat.png", []byte("hello"), 400, "bad picture"},
		{"too big", "office", "cat.png", append(pngBytes(t), make([]byte, sprite.MaxPictureBytes)...), 413, "limited"},
		{"unknown channel", "gym", "cat.png", pngBytes(t), 404, "no channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			rec := httptest.NewRecorder()
			f.srv.ServeHTTP(rec, upload(t, "/api/channels/"+tt.channel+"/pictures", tt.filename, tt.data))
			if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.contains) {
				t.Errorf("code %d body %s, want %d with %s", rec.Code, rec.Body, tt.code, tt.contains)
			}
		})
	}

	f := newFixture(t)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/channels/office/pictures", strings.NewReader("plain")))
	if rec.Code != 400 {
		t.Errorf("non-multipart upload: %d", rec.Code)
	}
	if _, err := f.pictures.Add("office", "cat.png", pngBytes(t)); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	f.srv.ServeHTTP(rec, upload(t, "/api/channels/office/pictures", "cat.png", pngBytes(t)))
	if rec.Code != http.StatusConflict {
		t.Errorf("second upload as cat.png: %d %s", rec.Code, rec.Body)
	}
	for _, s := range []struct {
		method, path string
		code         int
		contains     string
	}{
		{"GET", "/api/channels/office/pictures", 200, `"pictures":["cat.png"]`},
		{"DELETE", "/api/channels/office/pictures/..%2Fstate.json", 400, "bad file name"},
		{"DELETE", "/api/channels/office/pictures/dog.png", 404, ""},
		{"DELETE", "/api/channels/office/pictures/cat.png", 204, ""},
		{"GET", "/api/channels/office/pictures", 200, `"pictures":[]`},
	} {
		rec := f.do(t, s.method, s.path, "")
		if rec.Code != s.code || !strings.Contains(rec.Body.String(), s.contains) {
			t.Errorf("%s %s: code %d body %s", s.method, s.path, rec.Code, rec.Body)
		}
	}
}

func TestPNGs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		code int
	}{
		{"sprite", "/api/sprites/heart.png", 200},
		{"unknown sprite", "/api/sprites/dragon.png", 404},
		{"sprite without extension", "/api/sprites/heart", 404},
		{"preview", "/api/preview.png", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			rec := f.do(t, "GET", tt.path, "")
			if rec.Code != tt.code {
				t.Fatalf("code %d: %s", rec.Code, rec.Body)
			}
			if tt.code != 200 {
				return
			}
			img, err := png.Decode(rec.Body)
			if err != nil || rec.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("not a PNG: %v", err)
			}
			if b := img.Bounds(); b.Dx() != 160 || b.Dy() != 160 {
				t.Errorf("rendered %v, want 160x160", b)
			}
		})
	}
}

func TestPreviewFollowsTheScreen(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, before := f.hub.Preview()
	f.hub.Notify(hub.Alert{Content: display.ColorContent(color.RGBA{R: 255, A: 255}), Duration: time.Minute, Source: "office"})
	deadline := time.Now().Add(3 * time.Second)
	content, after := f.hub.Preview()
	for (after == before || content.Kind != display.Color) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		content, after = f.hub.Preview()
	}
	if content.Kind != display.Color {
		t.Fatalf("preview %d -> %d showing %s", before, after, content.Name)
	}
	img, err := png.Decode(f.do(t, "GET", "/api/preview.png", "").Body)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := img.At(80, 80).RGBA(); r>>8 != 255 || g != 0 || b != 0 {
		t.Errorf("preview pixel %d %d %d, want red", r>>8, g>>8, b>>8)
	}
}

func TestPreviewETag(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.do(t, "GET", "/api/preview.png", "")
	etag := first.Header().Get("ETag")
	req := httptest.NewRequest("GET", "/api/preview.png", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	if etag == "" || rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("etag %q, revalidation answered %d with %d bytes", etag, rec.Code, rec.Body.Len())
	}
}

func upload(t *testing.T, path, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/dot.png")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWebUI(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for _, path := range []string{"/", "/channels/office"} {
		rec := f.do(t, "GET", path, "")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<pixelhub-app>") {
			t.Errorf("GET %s: code %d, want the page", path, rec.Code)
		}
	}
	if rec := f.do(t, "GET", "/app.js", ""); rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Errorf("GET /app.js: code %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := f.do(t, "GET", "/api/status", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"channel":"office"`) {
		t.Errorf("GET /api/status: code %d body %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, "GET", "/api/nothing", ""); rec.Code != 404 || strings.Contains(rec.Body.String(), "pixelhub-app") {
		t.Errorf("GET /api/nothing: code %d body %s, want a 404 without the page", rec.Code, rec.Body)
	}
	if rec := f.do(t, "GET", "/healthz", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("GET /healthz: code %d", rec.Code)
	}
}

func TestPicturePNG(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "dot.png")
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	img.Set(3, 3, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(part, img); err != nil {
		t.Fatal(err)
	}
	form.Close()
	req := httptest.NewRequest("POST", "/api/channels/office/pictures", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	rec = f.do(t, "GET", "/api/channels/office/pictures/dot.png", "")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("thumbnail: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got, err := png.Decode(rec.Body); err != nil || got.Bounds().Dx() != 160 {
		t.Errorf("thumbnail is not a 160 px PNG: %v", err)
	}
	for _, path := range []string{"/api/channels/office/pictures/none.png", "/api/channels/nope/pictures/dot.png", "/api/channels/office/pictures/..%2Fx.png"} {
		if rec := f.do(t, "GET", path, ""); rec.Code < 400 {
			t.Errorf("GET %s: code %d, want an error", path, rec.Code)
		}
	}
}

type fakeGallery struct{}

func (fakeGallery) List(_ context.Context, category int, _ gallery.Sort, page, _ int) ([]gallery.Item, error) {
	if page > 1 {
		return nil, nil
	}
	return []gallery.Item{{Name: "cat", Author: "someone", Likes: 3, FileID: "group1/M00/aa"},
		{Name: "Corazón", Tags: []string{"love"}, Likes: 9, FileID: fmt.Sprintf("group1/M00/heart-%d", category)},
		{Name: "Super Mario Bros", Likes: 1, FileID: "group1/M00/mario"}}, nil
}

func (fakeGallery) Download(_ context.Context, file string) ([]byte, error) {
	if file == "group1/M00/aa" {
		return []byte("not a gallery file"), nil
	}
	frame := image.NewRGBA(image.Rect(0, 0, 16, 16))
	return gallery.Encode([]*image.RGBA{frame}, 100*time.Millisecond), nil
}

func TestFavorites(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	liked, err := favorites.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := (&Server{Hub: f.hub, Gallery: fakeGallery{}, Favorites: liked, Size: 16}).Handler()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	if rec := do("POST", "/api/favorites", `{"file":"group1/M00/mario","name":"Mario","likes":4}`); rec.Code != 201 || !strings.Contains(rec.Body.String(), `"name":"Mario"`) {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/favorites", `{"file":"group1/M00/aa","name":"broken"}`); rec.Code != 422 {
		t.Errorf("adding an unreadable file: %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/api/gallery/gif?file=group1/M00/mario", ""); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/gif" {
		t.Errorf("a favourite's gif: %d", rec.Code)
	}
	if rec := do("DELETE", "/api/favorites?file=group1/M00/mario", ""); rec.Code != 200 || strings.Contains(rec.Body.String(), "Mario") {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec := do("DELETE", "/api/favorites?file=group1/M00/mario", ""); rec.Code != 404 {
		t.Errorf("remove twice: %d", rec.Code)
	}
}

func TestGallery(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	srv := (&Server{Hub: f.hub, Sprites: nil, Gallery: fakeGallery{}, Size: 16, GalleryPace: time.Microsecond}).Handler()
	search := func(q string) string {
		for range 200 {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest("GET", "/api/gallery/search?q="+q, nil))
			if strings.Contains(rec.Body.String(), `"indexed":48,`) {
				return rec.Body.String()
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("the gallery index never finished")
		return ""
	}
	for q, want := range map[string]string{"corazon": `"name":"Corazón"`, "LOVE": `"name":"Corazón"`, "cat+someone": `"name":"cat"`, "dog": `"items":[]`, "ove": `"items":[]`, "mariobros": `"name":"Super Mario Bros"`, "bros": `"name":"Super Mario Bros"`} {
		if got := search(q); !strings.Contains(got, want) {
			t.Errorf("search %s: %s, want %s", q, got, want)
		}
	}
	tests := []struct {
		path     string
		code     int
		contains string
	}{
		{"/api/gallery/categories", 200, `"recommended"`},
		{"/api/gallery?category=animal", 200, `"file":"group1/M00/aa"`},
		{"/api/gallery?category=animal&page=2", 200, `"more":false`},
		{"/api/gallery?category=cars", 400, "category"},
		{"/api/gallery?category=animal&page=0", 400, "page"},
		{"/api/gallery/gif?file=group1/M00/aa", 422, "not a 16x16"},
		{"/api/gallery/search?q=", 400, "q:"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
		if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.contains) {
			t.Errorf("%s: code %d body %s, want %d with %s", tt.path, rec.Code, rec.Body, tt.code, tt.contains)
		}
	}
}
