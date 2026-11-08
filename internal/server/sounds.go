package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/PlatanosVerdes/pixelhub/internal/sounds"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

type Sounds interface {
	List() []sounds.Sound
	AddText(name, text string) (sounds.Sound, error)
	AddAudio(name string, audio []byte) (sounds.Sound, error)
	Get(id string) (sounds.Sound, []byte, error)
	Delete(id string) error
}

func (s *Server) soundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sounds", func(w http.ResponseWriter, _ *http.Request) { s.replySounds(w, http.StatusOK, nil) })
	mux.HandleFunc("POST /api/sounds", s.addSound)
	mux.HandleFunc("DELETE /api/sounds/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.replySounds(w, http.StatusOK, s.Sounds.Delete(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/sounds/{id}/play", s.playSound)
}

// addSound keeps a phrase, sent as JSON {"name", "text"}, or a recording, the multipart fields "name" and "audio".
func (s *Server) addSound(w http.ResponseWriter, r *http.Request) {
	kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if kind != "multipart/form-data" {
		var req struct {
			Name string `json:"name"`
			Text string `json:"text"`
		}
		if decode(w, r, &req, smallBody) {
			_, err := s.Sounds.AddText(req.Name, req.Text)
			s.replySounds(w, http.StatusCreated, err)
		}
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRecording+uploadOverhead)
	file, _, err := r.FormFile("audio")
	if err != nil {
		if tooBig := new(http.MaxBytesError); errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("recordings are limited to %d MB", maxRecording>>20))
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Errorf("want the recording in the field \"audio\": %w", err))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read upload: %w", err))
		return
	}
	if s.Decoder != nil {
		ctx, cancel := context.WithTimeout(r.Context(), talkWork)
		defer cancel()
		if _, err := s.Decoder.Decode(ctx, data, 1, ""); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("not a recording Mosko can play: %w", err))
			return
		}
	}
	_, err = s.Sounds.AddAudio(r.FormValue("name"), data)
	s.replySounds(w, http.StatusCreated, err)
}

// playSound makes Mosko say a kept sound, with the voice settings of now.
func (s *Server) playSound(w http.ResponseWriter, r *http.Request) {
	snd, audio, err := s.Sounds.Get(r.PathValue("id"))
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, sounds.ErrNotFound) {
			code = http.StatusNotFound
		}
		writeError(w, code, err)
		return
	}
	s.speak(w, r, func(ctx context.Context, tune state.Talk) (talk.PCM, []talk.Span, int, error) {
		if !snd.Audio {
			return s.spoken(ctx, snd.Text, tune)
		}
		pcm, err := s.Decoder.Decode(ctx, audio, tune.PitchRatio(), tune.Effect)
		if err != nil {
			return talk.PCM{}, nil, http.StatusInternalServerError, err
		}
		return pcm, nil, 0, nil
	})
}

func (s *Server) replySounds(w http.ResponseWriter, code int, err error) {
	switch {
	case errors.Is(err, sounds.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, sounds.ErrInvalid):
		writeError(w, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, code, map[string]any{"sounds": s.Sounds.List()})
	}
}
