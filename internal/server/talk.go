package server

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/hub"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

const (
	maxRecording = 12 << 20
	// talkWork is how long a request may take to read, turn into speech and answer.
	talkWork = 90 * time.Second
	// sendSlack covers the clip reaching the bridge.
	sendSlack = 2 * time.Second
)

// Voice reads text aloud.
type Voice interface {
	Say(ctx context.Context, text string) (talk.PCM, error)
}

// Decoder turns a recording into mono audio at a pitch.
type Decoder interface {
	Decode(ctx context.Context, audio []byte, pitch float64, effect string) (talk.PCM, error)
}

// talking lets one clip be made at a time: the Pi has no room for two speech runs at once.
type talking struct{ sync.Mutex }

type talkRequest struct {
	Text string `json:"text"`
}

// talk makes Mosko say a text, sent as JSON, or a recording, sent as the multipart field "audio".
func (s *Server) talk(w http.ResponseWriter, r *http.Request) {
	if s.Decoder == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("talking needs ffmpeg"))
		return
	}
	if !s.talking.TryLock() {
		writeError(w, http.StatusTooManyRequests, errors.New("Mosko is still on the last one"))
		return
	}
	defer s.talking.Unlock()
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(talkWork))
	_ = rc.SetWriteDeadline(time.Now().Add(talkWork))
	ctx, cancel := context.WithTimeout(r.Context(), talkWork)
	defer cancel()

	tune := s.Hub.State().Talk
	var pcm talk.PCM
	var spans []talk.Span
	var status int
	var err error
	if kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); kind == "multipart/form-data" {
		pcm, status, err = s.recording(ctx, w, r, tune)
	} else {
		var req talkRequest
		if !decode(w, r, &req, smallBody) {
			return
		}
		pcm, spans, status, err = s.spoken(ctx, req.Text, tune)
	}
	if err != nil {
		writeError(w, status, err)
		return
	}
	clip := talk.Clip(pcm, spans, tune.Lag())
	if err := s.Hub.Say(clip, talk.Length(clip)+sendSlack); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, hub.ErrLent) {
			code = http.StatusConflict
		}
		writeError(w, code, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"duration": pcm.Duration().Round(time.Millisecond).String(), "frames": len(clip.Frames)})
}

func (s *Server) recording(ctx context.Context, w http.ResponseWriter, r *http.Request, tune state.Talk) (talk.PCM, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRecording+uploadOverhead)
	file, _, err := r.FormFile("audio")
	if err != nil {
		if tooBig := new(http.MaxBytesError); errors.As(err, &tooBig) {
			return talk.PCM{}, http.StatusRequestEntityTooLarge, fmt.Errorf("recordings are limited to %d MB", maxRecording>>20)
		}
		return talk.PCM{}, http.StatusBadRequest, fmt.Errorf("want the recording in the field \"audio\": %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return talk.PCM{}, http.StatusBadRequest, fmt.Errorf("read upload: %w", err)
	}
	pcm, err := s.Decoder.Decode(ctx, data, tune.PitchRatio(), tune.Effect)
	if err != nil {
		return talk.PCM{}, http.StatusBadRequest, err
	}
	return pcm, 0, nil
}

// spoken reads text a sentence at a time, so the moods its endings set fall where they are said.
func (s *Server) spoken(ctx context.Context, text string, tune state.Talk) (talk.PCM, []talk.Span, int, error) {
	lines := talk.Script(text)
	if len(lines) == 0 {
		return talk.PCM{}, nil, http.StatusBadRequest, errors.New("nothing to say")
	}
	if s.Voice == nil {
		return talk.PCM{}, nil, http.StatusServiceUnavailable, errors.New("no speech server set up")
	}
	pcm, spans, err := talk.Speak(ctx, s.Voice, lines)
	if err != nil {
		return talk.PCM{}, nil, http.StatusBadGateway, err
	}
	if tune.PitchRatio() != 1 || tune.Effect != "" {
		out, err := s.Decoder.Decode(ctx, pcm.WAV(), tune.PitchRatio(), tune.Effect)
		if err != nil {
			return talk.PCM{}, nil, http.StatusInternalServerError, err
		}
		pcm, spans = out, talk.Stretch(spans, pcm.Duration(), out.Duration())
	}
	return pcm, spans, 0, nil
}

// moskoPNG draws Mosko mid-word, for the UI.
func (s *Server) moskoPNG(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=3600")
	_ = png.Encode(w, sprite.Resize(talk.Face(talk.Half, talk.Calm), 16*8))
}
