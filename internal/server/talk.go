package server

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/hub"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

const (
	maxRecording = 12 << 20
	// talkWork is how long a request may take to read, turn into speech and answer.
	talkWork = 90 * time.Second
	// sendSlack covers sending the frames over Bluetooth before the display starts them.
	sendSlack = 4 * time.Second
)

// Voice reads text aloud.
type Voice interface {
	Say(ctx context.Context, text string) (talk.PCM, error)
}

// Decoder turns a recording into mono audio at a pitch.
type Decoder interface {
	Decode(ctx context.Context, audio []byte, pitch float64) (talk.PCM, error)
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
	var status int
	var err error
	if kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); kind == "multipart/form-data" {
		pcm, status, err = s.recording(ctx, w, r, tune.PitchRatio())
	} else {
		var req talkRequest
		if !decode(w, r, &req, smallBody) {
			return
		}
		pcm, status, err = s.spoken(ctx, req.Text, tune.PitchRatio())
	}
	if err != nil {
		writeError(w, status, err)
		return
	}
	clip := talk.Clip(pcm, time.Duration(tune.LagMS)*time.Millisecond)
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

func (s *Server) recording(ctx context.Context, w http.ResponseWriter, r *http.Request, pitch float64) (talk.PCM, int, error) {
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
	pcm, err := s.Decoder.Decode(ctx, data, pitch)
	if err != nil {
		return talk.PCM{}, http.StatusBadRequest, err
	}
	return pcm, 0, nil
}

func (s *Server) spoken(ctx context.Context, text string, pitch float64) (talk.PCM, int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return talk.PCM{}, http.StatusBadRequest, errors.New("nothing to say")
	}
	if s.Voice == nil {
		return talk.PCM{}, http.StatusServiceUnavailable, errors.New("no speech server set up")
	}
	pcm, err := s.Voice.Say(ctx, text)
	if err != nil {
		return talk.PCM{}, http.StatusBadGateway, err
	}
	if pitch != 1 {
		if pcm, err = s.Decoder.Decode(ctx, pcm.WAV(), pitch); err != nil {
			return talk.PCM{}, http.StatusInternalServerError, err
		}
	}
	return pcm, 0, nil
}

// moskoPNG draws Mosko mid-word, for the UI.
func (s *Server) moskoPNG(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=3600")
	_ = png.Encode(w, sprite.Resize(talk.Face(talk.Half), 16*8))
}
