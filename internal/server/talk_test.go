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
	if rec.Code != 202 || said != "hola" || pitch != 0 {
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
