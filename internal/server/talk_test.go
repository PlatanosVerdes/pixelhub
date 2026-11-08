package server

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

type fakeVoice struct{ said *string }

func (v fakeVoice) Say(_ context.Context, text string) (talk.PCM, error) {
	*v.said = text
	return talk.PCM{Rate: talk.Rate, Samples: make([]int16, talk.Rate)}, nil
}

type fakeDecoder struct {
	pitch  *float64
	effect *string
}

func (d fakeDecoder) Decode(_ context.Context, audio []byte, pitch float64, effect string) (talk.PCM, error) {
	*d.pitch = pitch
	if d.effect != nil {
		*d.effect = effect
	}
	if string(audio) == "noise" {
		return talk.PCM{}, errors.New("no sound in that file")
	}
	return talk.PCM{Rate: talk.Rate, Samples: make([]int16, talk.Rate/2)}, nil
}

func TestTalk(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var said string
	var pitch float64
	var effect string
	srv := (&Server{Hub: f.hub, Voice: fakeVoice{&said}, Decoder: fakeDecoder{&pitch, &effect}, Size: 16}).Handler()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk", strings.NewReader(`{"text":"  hola  "}`)))
	if rec.Code != 202 || said != "hola" || pitch != 0 || !strings.Contains(rec.Body.String(), `"faces":[{"shape":0,"mood":0,"ms":`) {
		t.Fatalf("text: code %d body %s, said %q, decoded at %v", rec.Code, rec.Body, said, pitch)
	}
	for deadline := time.Now().Add(2 * time.Second); f.hub.Status().Showing != "Mosko"; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("showing %q, want Mosko", f.hub.Status().Showing)
		}
	}

	if rec := f.do(t, "PUT", "/api/settings", `{"talk":{"lag_ms":200,"pitch":130,"effect":"robot"}}`); rec.Code != 200 {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk", strings.NewReader(`{"text":"otra"}`)))
	if rec.Code != 202 || pitch != 1.3 || effect != "robot" {
		t.Errorf("a higher robot voice: code %d, decoded at %v as %q", rec.Code, pitch, effect)
	}
	if rec := f.do(t, "PUT", "/api/settings", `{"talk":{"effect":"opera"}}`); rec.Code != 400 {
		t.Errorf("an unknown effect: %d %s", rec.Code, rec.Body)
	}

	upload := func(data string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("audio", "nota.m4a")
		part.Write([]byte(data))
		w.Close()
		req := httptest.NewRequest("POST", "/api/talk", &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	if rec := upload("voice"); rec.Code != 202 || !strings.Contains(rec.Body.String(), `"duration":"500ms"`) {
		t.Errorf("recording: %d %s", rec.Code, rec.Body)
	}
	if rec := upload("noise"); rec.Code != 400 || !strings.Contains(rec.Body.String(), "no sound") {
		t.Errorf("a file without sound: %d %s", rec.Code, rec.Body)
	}
	for _, body := range []string{`{"text":"   "}`, `{"words":"hola"}`} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk", strings.NewReader(body)))
		if rec.Code != 400 {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}

	f.hub.Release()
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk", strings.NewReader(`{"text":"hola"}`)))
	if rec.Code != 409 {
		t.Errorf("while lent to the phone: %d %s", rec.Code, rec.Body)
	}
}

func TestMoskoFaces(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for path, code := range map[string]int{"/api/talk/mosko.png": 200, "/api/talk/mosko.png?shape=4&mood=4": 200, "/api/talk/mosko.png?shape=5": 400, "/api/talk/mosko.png?mood=x": 400} {
		if rec := f.do(t, "GET", path, ""); rec.Code != code {
			t.Errorf("%s: %d, want %d", path, rec.Code, code)
		}
	}
}

func TestTalkWithoutTools(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var pitch float64
	for _, s := range []*Server{{Hub: f.hub}, {Hub: f.hub, Decoder: fakeDecoder{pitch: &pitch}}} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk", strings.NewReader(`{"text":"hola"}`)))
		if rec.Code != 503 {
			t.Errorf("code %d body %s, want 503", rec.Code, rec.Body)
		}
	}
}

func TestIdleAndKeptSpeech(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var said string
	var pitch float64
	srv := (&Server{Hub: f.hub, Voice: fakeVoice{&said}, Decoder: fakeDecoder{pitch: &pitch}, Size: 16}).Handler()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk/idle?for=30s", nil))
	if rec.Code != 202 {
		t.Fatalf("idle: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(2 * time.Second); f.hub.Status().Showing != "Mosko"; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("showing %q, want Mosko waiting", f.hub.Status().Showing)
		}
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk/idle?for=1h", nil))
	if rec.Code != 400 {
		t.Errorf("idle for an hour: %d %s", rec.Code, rec.Body)
	}

	for i, want := range []string{"¡Hola!", ""} {
		said = ""
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("POST", "/api/talk", strings.NewReader(`{"text":"¡Hola!"}`)))
		if rec.Code != 202 || said != want {
			t.Errorf("say %d: code %d, read aloud %q, want %q", i+1, rec.Code, said, want)
		}
	}
}

func TestSpeechKeptOnDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var said string
	first := newKeptVoice(fakeVoice{&said}, dir)
	if _, err := first.Say(context.Background(), "hola"); err != nil || said != "hola" {
		t.Fatalf("first read: %v, said %q", err, said)
	}
	said = ""
	again := newKeptVoice(fakeVoice{&said}, dir)
	p, err := again.Say(context.Background(), "hola")
	if err != nil || said != "" || p.Rate != talk.Rate || len(p.Samples) != talk.Rate {
		t.Errorf("after a restart: %v, read aloud %q, got %d Hz %d samples", err, said, p.Rate, len(p.Samples))
	}
}

func TestPranks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var pitch float64
	srv := (&Server{Hub: f.hub, Decoder: fakeDecoder{pitch: &pitch}, Size: 16}).Handler()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	for _, body := range []string{`{"kinds":[]}`, `{"kinds":["sneeze"]}`, `{"kinds":["fart"],"for":"5h"}`, `{"kinds":["fart"],"every":"always"}`} {
		if rec := do("POST", "/api/talk/pranks", body); rec.Code != 400 {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	rec := do("POST", "/api/talk/pranks", `{"kinds":["scream","fart","fart"],"for":"30m","every":"often"}`)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"on":true,"kinds":["fart","scream"],"every":"often"`) {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/talk/noise", `{"kind":"burp"}`); rec.Code != 202 || !strings.Contains(rec.Body.String(), `"faces":[{`) {
		t.Fatalf("a burp now: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(2 * time.Second); f.hub.Status().Showing != "Mosko"; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("showing %q, want Mosko", f.hub.Status().Showing)
		}
	}
	if rec := do("POST", "/api/talk/noise", `{"kind":"sneeze"}`); rec.Code != 400 {
		t.Errorf("an unknown noise: %d %s", rec.Code, rec.Body)
	}
	if rec := do("DELETE", "/api/talk/pranks", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"on":false,"kinds":[]`) {
		t.Errorf("stop: %d %s", rec.Code, rec.Body)
	}
}
