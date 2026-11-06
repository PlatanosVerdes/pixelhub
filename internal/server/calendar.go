package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/calendar"
)

const calendarShow = 15 * time.Second

type Calendar interface {
	Status() calendar.Status
	Next() (calendar.Event, bool)
	Set(ctx context.Context, address string, leadMinutes int) error
	Forget() error
}

func (s *Server) calendarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/calendar", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Calendar.Status())
	})
	mux.HandleFunc("PUT /api/calendar", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL         string `json:"url"`
			LeadMinutes int    `json:"lead_minutes"`
		}
		if !decode(w, r, &req, smallBody) {
			return
		}
		if err := s.Calendar.Set(r.Context(), req.URL, req.LeadMinutes); err != nil {
			code := http.StatusBadRequest
			if errors.Is(err, calendar.ErrNotConfigured) {
				code = http.StatusConflict
			}
			writeError(w, code, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Calendar.Status())
	})
	mux.HandleFunc("DELETE /api/calendar", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.Calendar.Forget(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Calendar.Status())
	})
	// Shows the warning the next event will get, or a sample one, to see how it looks.
	mux.HandleFunc("POST /api/calendar/show", func(w http.ResponseWriter, _ *http.Request) {
		left := time.Duration(s.Calendar.Status().LeadMinutes) * time.Minute
		if e, ok := s.Calendar.Next(); ok {
			left = min(left, time.Until(e.Start))
		}
		s.Hub.Show(calendar.Picture(left, s.Size), calendarShow)
		writeJSON(w, http.StatusAccepted, map[string]any{"showing": "calendar", "duration": calendarShow.String()})
	})
}
