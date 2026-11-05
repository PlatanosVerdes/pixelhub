package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/spotify"
)

type Spotify interface {
	Content() (display.Content, string, bool)
	Status() spotify.Status
	SetClientID(id string) error
	LoginURL(redirect string) (string, error)
	Finish(ctx context.Context, state, code string) error
	Forget() error
}

func (s *Server) spotifyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/spotify", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Spotify.Status())
	})
	mux.HandleFunc("PUT /api/spotify", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClientID string `json:"client_id"`
		}
		if !decode(w, r, &req, smallBody) {
			return
		}
		if err := s.Spotify.SetClientID(req.ClientID); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Spotify.Status())
	})
	mux.HandleFunc("DELETE /api/spotify", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.Spotify.Forget(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Spotify.Status())
	})
	// The cover as the display shows it, also while paused; 404 when there is none.
	mux.HandleFunc("GET /api/spotify/cover.png", func(w http.ResponseWriter, _ *http.Request) {
		c, _, _ := s.Spotify.Content()
		if len(c.Frames) == 0 {
			writeError(w, http.StatusNotFound, errors.New("no cover"))
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		writePNG(w, c.Frames[0].Image)
	})
	mux.HandleFunc("GET /api/spotify/login", func(w http.ResponseWriter, r *http.Request) {
		to, err := s.Spotify.LoginURL(callbackURL(r))
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, spotify.ErrNotConfigured) {
				code = http.StatusConflict
			}
			writeError(w, code, err)
			return
		}
		http.Redirect(w, r, to, http.StatusFound)
	})
	mux.HandleFunc("GET /api/spotify/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		problem := q.Get("error")
		if problem == "" {
			if err := s.Spotify.Finish(r.Context(), q.Get("state"), q.Get("code")); err != nil {
				problem = err.Error()
			}
		}
		back := "/#settings"
		if problem != "" {
			back = "/?spotify_error=" + url.QueryEscape(problem) + "#settings"
		}
		http.Redirect(w, r, back, http.StatusFound)
	})
}

// callbackURL is where Spotify sends the browser back: this same host, which the Spotify app must
// list. Spotify refuses "localhost" but takes the loopback address.
func callbackURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if h, port, err := net.SplitHostPort(host); err == nil && h == "localhost" {
		host = net.JoinHostPort("127.0.0.1", port)
	} else if host == "localhost" {
		host = "127.0.0.1"
	}
	return scheme + "://" + host + "/api/spotify/callback"
}
