package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"net/http"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/hub"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
)

const (
	smallBody = 4 << 10
	largeBody = 64 << 10

	defaultAlert = 10 * time.Second
	maxAlert     = time.Hour
	maxPriority  = 10
)

type Hub interface {
	Notify(hub.Alert) (channel string, accepted bool)
	SetChannel(name string, d time.Duration) error
	ClearChannel()
	Release() time.Time
	Claim()
	Status() hub.Status
	State() state.State
	Update(change func(*state.State) error) (state.State, error)
	AddChannel(c state.Channel) (state.State, error)
	ReplaceChannel(name string, c state.Channel) (state.State, error)
	DeleteChannel(name string) (state.State, error)
	ListPictures(channel string) ([]string, error)
	AddPicture(channel, name string, data []byte) (string, error)
	RemovePicture(channel, file string) error
	Preview() (display.Content, int)
}

type Sprites interface {
	hub.Sprites
	Names() []string
}

type Server struct {
	Hub     Hub
	Sprites Sprites
	// Size is the display's side in pixels, for drawing previews.
	Size    int
	Version string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
	})
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/notify", s.notify)
	mux.HandleFunc("POST /api/channel", s.setChannel)
	mux.HandleFunc("DELETE /api/channel", s.clearChannel)
	mux.HandleFunc("POST /api/release", s.release)
	mux.HandleFunc("POST /api/claim", s.claim)
	mux.HandleFunc("GET /api/sprites", s.sprites)
	mux.HandleFunc("GET /api/sprites/{file}", s.spritePNG)
	mux.HandleFunc("GET /api/preview.png", s.previewPNG)

	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("GET /api/channels", s.listChannels)
	mux.HandleFunc("POST /api/channels", s.createChannel)
	mux.HandleFunc("PUT /api/channels/{name}", s.updateChannel)
	mux.HandleFunc("DELETE /api/channels/{name}", s.deleteChannel)
	mux.HandleFunc("PUT /api/schedule", s.putSchedule)

	mux.HandleFunc("GET /api/channels/{name}/pictures", s.listPictures)
	mux.HandleFunc("POST /api/channels/{name}/pictures", s.addPicture)
	mux.HandleFunc("DELETE /api/channels/{name}/pictures/{file}", s.removePicture)
	return mux
}

type notifyRequest struct {
	Sprite   string `json:"sprite"`
	Color    []int  `json:"color"`
	Priority int    `json:"priority"`
	Duration string `json:"duration"`
	Source   string `json:"source"`
}

func (s *Server) notify(w http.ResponseWriter, r *http.Request) {
	var req notifyRequest
	if !decode(w, r, &req, smallBody) {
		return
	}
	alert, err := s.alert(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	channel, accepted := s.Hub.Notify(alert)
	if !accepted {
		writeJSON(w, http.StatusOK, map[string]any{"dropped": true, "channel": channel, "source": req.Source})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"showing": alert.Content.Name, "duration": alert.Duration.String(), "channel": channel})
}

type channelRequest struct {
	Name string `json:"name"`
	For  string `json:"for"`
}

func (s *Server) setChannel(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if !decode(w, r, &req, smallBody) {
		return
	}
	var d time.Duration
	if req.For != "" {
		var err error
		if d, err = time.ParseDuration(req.For); err != nil || d <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("for %q: want something like 4h", req.For))
			return
		}
	}
	if err := s.Hub.SetChannel(req.Name, d); err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, s.Hub.Status())
}

func (s *Server) clearChannel(w http.ResponseWriter, _ *http.Request) {
	s.Hub.ClearChannel()
	writeJSON(w, http.StatusOK, s.Hub.Status())
}

func decode(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("body: %w", err))
		return false
	}
	return true
}

func (s *Server) alert(req notifyRequest) (hub.Alert, error) {
	a := hub.Alert{Priority: req.Priority, Duration: defaultAlert, Source: req.Source}
	if req.Priority < 0 || req.Priority > maxPriority {
		return a, fmt.Errorf("priority %d is outside 0-%d", req.Priority, maxPriority)
	}
	if req.Duration != "" {
		d, err := time.ParseDuration(req.Duration)
		if err != nil || d <= 0 || d > maxAlert {
			return a, fmt.Errorf("duration %q: want something like 10s, at most %s", req.Duration, maxAlert)
		}
		a.Duration = d
	}
	content, err := s.content(req)
	a.Content = content
	return a, err
}

// content turns the request into what to show; each way of describing it (sprite, color) is one case.
func (s *Server) content(req notifyRequest) (display.Content, error) {
	switch {
	case req.Sprite != "" && req.Color != nil:
		return display.Content{}, errors.New("give sprite or color, not both")
	case req.Sprite != "":
		c, ok := s.Sprites.Get(req.Sprite)
		if !ok {
			return display.Content{}, fmt.Errorf("no sprite named %q, see GET /sprites", req.Sprite)
		}
		return c, nil
	case req.Color != nil:
		if len(req.Color) != 3 {
			return display.Content{}, errors.New("color: want [r, g, b]")
		}
		var rgb [3]uint8
		for i, v := range req.Color {
			if v < 0 || v > 255 {
				return display.Content{}, fmt.Errorf("color: %d is outside 0-255", v)
			}
			rgb[i] = uint8(v)
		}
		return display.ColorContent(color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 255}), nil
	default:
		return display.Content{}, errors.New("give a sprite or a color")
	}
}

func (s *Server) release(w http.ResponseWriter, _ *http.Request) {
	until := s.Hub.Release()
	writeJSON(w, http.StatusOK, map[string]any{"released_until": until})
}

func (s *Server) claim(w http.ResponseWriter, _ *http.Request) {
	s.Hub.Claim()
	writeJSON(w, http.StatusOK, map[string]any{"claimed": true})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Hub.Status())
}

func (s *Server) sprites(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]string{"sprites": s.Sprites.Names()})
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, state.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, state.ErrConflict), errors.Is(err, sprite.ErrExists):
		return http.StatusConflict
	case errors.Is(err, state.ErrInvalid), errors.Is(err, sprite.ErrBadPicture), errors.Is(err, sprite.ErrBadName):
		return http.StatusBadRequest
	case errors.Is(err, sprite.ErrTooBig):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
