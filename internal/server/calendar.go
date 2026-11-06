package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/calendar"
)

const calendarShow = 15 * time.Second

type Calendar interface {
	Status() calendar.Status
	Next() (calendar.Event, bool)
	SetClient(id, secret string) error
	Select(ids []string) error
	LoginURL(redirect string) (string, error)
	Finish(ctx context.Context, state, code string) error
	Forget() error
}

func (s *Server) calendarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/google", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Calendar.Status())
	})
	mux.HandleFunc("PUT /api/google", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if !decode(w, r, &req, smallBody) {
			return
		}
		if err := s.Calendar.SetClient(req.ClientID, req.ClientSecret); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Calendar.Status())
	})
	mux.HandleFunc("PUT /api/google/calendars", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IDs []string `json:"ids"`
		}
		if !decode(w, r, &req, smallBody) {
			return
		}
		if err := s.Calendar.Select(req.IDs); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Calendar.Status())
	})
	mux.HandleFunc("DELETE /api/google", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.Calendar.Forget(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Calendar.Status())
	})
	mux.HandleFunc("GET /api/google/login", func(w http.ResponseWriter, r *http.Request) {
		to, err := s.Calendar.LoginURL(callbackURL(r, "/api/google/callback"))
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, calendar.ErrNotConfigured) {
				code = http.StatusConflict
			}
			writeError(w, code, err)
			return
		}
		http.Redirect(w, r, to, http.StatusFound)
	})
	mux.HandleFunc("GET /api/google/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		problem := q.Get("error")
		if problem == "" {
			if err := s.Calendar.Finish(r.Context(), q.Get("state"), q.Get("code")); err != nil {
				problem = err.Error()
			}
		}
		back := "/#settings"
		if problem != "" {
			back = "/?google_error=" + url.QueryEscape(problem) + "#settings"
		}
		http.Redirect(w, r, back, http.StatusFound)
	})
	// Shows the warning the next event will get, or a sample one, to see how it looks.
	mux.HandleFunc("POST /api/google/show", func(w http.ResponseWriter, _ *http.Request) {
		left, title := 10*time.Minute, "Example event"
		if e, ok := s.Calendar.Next(); ok {
			left, title = time.Until(e.Start), e.Title
			if len(e.Alarms) > 0 {
				left = min(left, time.Duration(e.Alarms[0])*time.Minute)
			}
		}
		s.Hub.Show(calendar.Picture(left, title, s.Size), calendarShow)
		writeJSON(w, http.StatusAccepted, map[string]any{"showing": "calendar", "duration": calendarShow.String()})
	})
}
