package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/drawings"
	"github.com/PlatanosVerdes/pixelhub/internal/gallery"
	"github.com/PlatanosVerdes/pixelhub/internal/lists"
)

// drawingBody fits MaxFrames frames of 16x16 written as hex, with room to spare.
const drawingBody = 512 << 10

type Drawings interface {
	List() []drawings.Drawing
	Get(id string) (drawings.Drawing, error)
	Save(d drawings.Drawing) (drawings.Drawing, error)
	Delete(id string) error
}

func (s *Server) drawingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/drawings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"drawings": s.Drawings.List()})
	})
	mux.HandleFunc("POST /api/drawings", func(w http.ResponseWriter, r *http.Request) {
		var d drawings.Drawing
		if decode(w, r, &d, drawingBody) {
			d.ID = ""
			s.replyDrawing(w, http.StatusCreated, d)
		}
	})
	mux.HandleFunc("PUT /api/drawings/{id}", func(w http.ResponseWriter, r *http.Request) {
		var d drawings.Drawing
		if decode(w, r, &d, drawingBody) {
			d.ID = r.PathValue("id")
			s.replyDrawing(w, http.StatusOK, d)
		}
	})
	mux.HandleFunc("DELETE /api/drawings/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Drawings.Delete(r.PathValue("id")); err != nil {
			writeError(w, drawingStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"drawings": s.Drawings.List()})
	})
	mux.HandleFunc("POST /api/drawings/show", s.showDrawing)
	if s.Lists != nil {
		mux.HandleFunc("POST /api/drawings/{id}/list", s.listDrawing)
	}
}

func (s *Server) replyDrawing(w http.ResponseWriter, code int, d drawings.Drawing) {
	kept, err := s.Drawings.Save(d)
	if err != nil {
		writeError(w, drawingStatus(err), err)
		return
	}
	writeJSON(w, code, map[string]any{"drawing": kept, "drawings": s.Drawings.List()})
}

type drawingShow struct {
	drawings.Drawing
	For string `json:"for"`
}

// showDrawing shows frames now, saved or not, like a gallery picture.
func (s *Server) showDrawing(w http.ResponseWriter, r *http.Request) {
	var req drawingShow
	if !decode(w, r, &req, drawingBody) {
		return
	}
	if req.Name == "" {
		req.Name = "drawing"
	}
	d := defaultShow
	if req.For != "" {
		var err error
		if d, err = time.ParseDuration(req.For); err != nil || d <= 0 || d > maxAlert {
			writeError(w, http.StatusBadRequest, fmt.Errorf("for %q: want something like 30s, at most %s", req.For, maxAlert))
			return
		}
	}
	if err := req.Check(s.Size); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	content := s.drawingContent(req.Drawing)
	s.Hub.Show(content, d)
	writeJSON(w, http.StatusAccepted, map[string]any{"showing": content.Name, "duration": d.String()})
}

// drawingContent keys the content by its pixels, so a changed drawing is sent again.
func (s *Server) drawingContent(d drawings.Drawing) display.Content {
	sum := sha256.Sum256([]byte(fmt.Sprint(d.DelayMS, strings.Join(d.Frames, ""))))
	c := display.Content{Kind: display.Picture, Name: d.Name, Key: "drawing/" + hex.EncodeToString(sum[:8])}
	for _, img := range d.Images(s.Size) {
		delay := d.Delay()
		if len(d.Frames) == 1 {
			delay = 0
		}
		c.Frames = append(c.Frames, display.Frame{Image: img, Delay: delay})
	}
	return c
}

// listDrawing puts a saved drawing in a list, so the automatic rotation shows it; again after an edit updates it.
func (s *Server) listDrawing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		List string `json:"list"`
	}
	if !decode(w, r, &req, smallBody) {
		return
	}
	d, err := s.Drawings.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, drawingStatus(err), err)
		return
	}
	b, err := gallery.GIF(d.Images(s.Size), d.Delay())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.replyLists(w, http.StatusCreated, s.Lists.Add(req.List, lists.Picture{File: "drawing/" + d.ID, Name: d.Name}, b))
}

func drawingStatus(err error) int {
	switch {
	case errors.Is(err, drawings.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, drawings.ErrInvalid):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
