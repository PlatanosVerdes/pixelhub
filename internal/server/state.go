package server

import (
	"fmt"
	"io"
	"net/http"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
)

func (s *Server) getState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Hub.State())
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, smallBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("body: %w", err))
		return
	}
	s.update(w, func(st *state.State) error { return st.ApplySettings(body) })
}

func (s *Server) listChannels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Hub.State().Channels)
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var c state.Channel
	if !decode(w, r, &c, largeBody) {
		return
	}
	st, err := s.Hub.AddChannel(c)
	reply(w, st, err)
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	var c state.Channel
	if !decode(w, r, &c, largeBody) {
		return
	}
	st, err := s.Hub.ReplaceChannel(r.PathValue("name"), c)
	reply(w, st, err)
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	st, err := s.Hub.DeleteChannel(r.PathValue("name"))
	reply(w, st, err)
}

func (s *Server) putSchedule(w http.ResponseWriter, r *http.Request) {
	var slots []state.Slot
	if !decode(w, r, &slots, largeBody) {
		return
	}
	s.update(w, func(st *state.State) error {
		st.Schedule = slots
		return nil
	})
}

func (s *Server) modes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"styles": display.Styles, "max_score": display.MaxScore})
}

func (s *Server) putMode(w http.ResponseWriter, r *http.Request) {
	var v display.View
	if !decode(w, r, &v, smallBody) {
		return
	}
	if err := v.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.update(w, func(st *state.State) error {
		st.Mode = v
		return nil
	})
}

func (s *Server) clearMode(w http.ResponseWriter, _ *http.Request) {
	s.update(w, func(st *state.State) error {
		st.Mode = display.View{}
		return nil
	})
}

func (s *Server) update(w http.ResponseWriter, change func(*state.State) error) {
	st, err := s.Hub.Update(change)
	reply(w, st, err)
}

func reply(w http.ResponseWriter, st state.State, err error) {
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
