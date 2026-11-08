package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/hub"
	"github.com/PlatanosVerdes/pixelhub/internal/talk"
)

const (
	pranksFor    = time.Hour
	maxPranksFor = 4 * time.Hour
	// pranksStart gets the first noise in soon, so it is plain the pranks are on.
	pranksStart = 20 * time.Second
)

// pranksEvery are how often Mosko makes a noise, as the shortest and longest wait between two.
var pranksEvery = map[string][2]time.Duration{
	"often":     {time.Minute, 4 * time.Minute},
	"sometimes": {3 * time.Minute, 10 * time.Minute},
	"rarely":    {8 * time.Minute, 20 * time.Minute},
}

type pranks struct {
	mu    sync.Mutex
	kinds []string
	every string
	until time.Time
	next  time.Time
	stop  context.CancelFunc
}

type pranksState struct {
	On    bool      `json:"on"`
	Kinds []string  `json:"kinds"`
	Every string    `json:"every,omitempty"`
	Until time.Time `json:"until,omitzero"`
	Next  time.Time `json:"next,omitzero"`
}

func (p *pranks) state() pranksState {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stop == nil {
		return pranksState{Kinds: []string{}}
	}
	return pranksState{On: true, Kinds: p.kinds, Every: p.every, Until: p.until, Next: p.next}
}

func (p *pranks) halt() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stop != nil {
		p.stop()
	}
	p.stop, p.kinds, p.every = nil, nil, ""
}

// end stops the pranks that ran with ctx, unless newer ones replaced them.
func (p *pranks) end(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ctx.Err() == nil {
		p.stop()
		p.stop, p.kinds, p.every = nil, nil, ""
	}
}

func (s *Server) pranksRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/talk/pranks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.pranks.state())
	})
	mux.HandleFunc("POST /api/talk/pranks", s.startPranks)
	mux.HandleFunc("DELETE /api/talk/pranks", func(w http.ResponseWriter, _ *http.Request) {
		s.pranks.halt()
		writeJSON(w, http.StatusOK, s.pranks.state())
	})
	mux.HandleFunc("POST /api/talk/noise", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Kind string `json:"kind"`
		}
		if !decode(w, r, &req, smallBody) {
			return
		}
		clip, poses, err := s.noise(req.Kind)
		if err != nil {
			writeError(w, noiseStatus(err), err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"made": req.Kind, "faces": poses, "faces_after_ms": clip.FramesAfter.Milliseconds()})
	})
}

type pranksRequest struct {
	Kinds []string `json:"kinds"`
	For   string   `json:"for"`
	Every string   `json:"every"`
}

// startPranks has Mosko make the noises asked for, one at a random moment after another, for a while.
func (s *Server) startPranks(w http.ResponseWriter, r *http.Request) {
	var req pranksRequest
	if !decode(w, r, &req, smallBody) {
		return
	}
	slices.Sort(req.Kinds)
	req.Kinds = slices.Compact(req.Kinds)
	if len(req.Kinds) == 0 || slices.ContainsFunc(req.Kinds, func(k string) bool { return !slices.Contains(talk.NoiseKinds, k) }) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("kinds %v: want some of %v", req.Kinds, talk.NoiseKinds))
		return
	}
	d := pranksFor
	if req.For != "" {
		var err error
		if d, err = time.ParseDuration(req.For); err != nil || d <= 0 || d > maxPranksFor {
			writeError(w, http.StatusBadRequest, fmt.Errorf("for %q: want something like 1h, at most %s", req.For, maxPranksFor))
			return
		}
	}
	if req.Every == "" {
		req.Every = "sometimes"
	}
	wait, ok := pranksEvery[req.Every]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("every %q: want often, sometimes or rarely", req.Every))
		return
	}
	s.pranks.halt()
	ctx, stop := context.WithCancel(context.Background())
	now := time.Now()
	s.pranks.mu.Lock()
	s.pranks.kinds, s.pranks.every, s.pranks.until, s.pranks.stop = req.Kinds, req.Every, now.Add(d), stop
	s.pranks.next = now.Add(min(pranksStart, wait[0]))
	s.pranks.mu.Unlock()
	go s.runPranks(ctx, wait)
	writeJSON(w, http.StatusAccepted, s.pranks.state())
}

func (s *Server) runPranks(ctx context.Context, wait [2]time.Duration) {
	for {
		s.pranks.mu.Lock()
		next, until, kinds := s.pranks.next, s.pranks.until, s.pranks.kinds
		s.pranks.mu.Unlock()
		if next.After(until) {
			s.pranks.end(ctx)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		kind := kinds[rand.IntN(len(kinds))]
		if _, _, err := s.noise(kind); err != nil {
			slog.Info("no prank this time", "kind", kind, "err", err)
		}
		s.pranks.mu.Lock()
		s.pranks.next = time.Now().Add(wait[0] + rand.N(wait[1]-wait[0]))
		s.pranks.mu.Unlock()
	}
}

var errMoskoBusy = errors.New("Mosko is still on the last one")

// noise has Mosko make a noise of kind now, unless he is already talking.
func (s *Server) noise(kind string) (display.Content, []talk.Pose, error) {
	if !s.talking.TryLock() {
		return display.Content{}, nil, errMoskoBusy
	}
	defer s.talking.Unlock()
	n, err := talk.MakeNoise(kind)
	if err != nil {
		return display.Content{}, nil, err
	}
	clip, poses := n.Clip(s.Hub.State().Talk.Lag())
	return clip, poses, s.Hub.Say(clip, talk.Length(clip)+sendSlack)
}

func noiseStatus(err error) int {
	switch {
	case errors.Is(err, errMoskoBusy):
		return http.StatusTooManyRequests
	case errors.Is(err, hub.ErrLent):
		return http.StatusConflict
	case errors.Is(err, talk.ErrNoNoise):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
