package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/gallery"
)

// The two lists that are not lists: gallery pictures picked at random, and ones that suit the moment.
const (
	Random = "@random"
	Moment = "@moment"
)

const (
	surpriseReady = 3
	// A picture for the moment is picked for the hour it was fetched in.
	surpriseFresh = 30 * time.Minute
	// Pictures are fetched ahead only while someone has asked for them lately.
	surpriseWanted = 3 * time.Hour
	surprisePace   = 4 * time.Second
	surpriseRecent = 60
	// The best liked of what a search finds are the ones worth showing.
	surpriseTop = 24
)

var randomCategories = []string{"recommended", "top", "character", "animal", "nature", "festival", "food", "emoji", "creative", "daily", "plant", "symbol"}

type surprise struct {
	content display.Content
	item    gallery.Item
	theme   string
	at      time.Time
}

type surprises struct {
	mu     sync.Mutex
	ready  map[string][]surprise
	wanted map[string]time.Time
	recent []string
	wake   chan struct{}
}

func (u *surprises) init() {
	if u.ready == nil {
		u.ready, u.wanted, u.wake = map[string][]surprise{}, map[string]time.Time{}, make(chan struct{}, 1)
	}
}

func (u *surprises) poke() {
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

// pop takes the next fresh picture of kind and asks for another; ok is false when none is ready.
func (u *surprises) pop(kind string, now time.Time) (surprise, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.init()
	u.wanted[kind] = now
	defer u.poke()
	q := slices.DeleteFunc(u.ready[kind], func(p surprise) bool { return kind == Moment && now.Sub(p.at) > surpriseFresh })
	if len(q) == 0 {
		u.ready[kind] = q
		return surprise{}, false
	}
	u.ready[kind] = q[1:]
	return q[0], true
}

// short is the kind that most needs a picture fetched, if any is wanted and short.
func (u *surprises) short(now time.Time) (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.init()
	for _, kind := range []string{Moment, Random} {
		if now.Sub(u.wanted[kind]) < surpriseWanted && len(u.ready[kind]) < surpriseReady {
			return kind, true
		}
	}
	return "", false
}

func (u *surprises) keep(kind string, p surprise) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.init()
	u.ready[kind] = append(u.ready[kind], p)
}

func (u *surprises) seen(file string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Contains(u.recent, file)
}

func (u *surprises) remember(file string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.recent = append(u.recent, file)
	if len(u.recent) > surpriseRecent {
		u.recent = u.recent[1:]
	}
}

// Surprise is the next picture of Random or Moment for the display's own turn; it never waits for
// the gallery, and gives none while the first ones are still being fetched.
func (s *Server) Surprise(kind string) (display.Content, bool, error) {
	if s.Gallery == nil {
		return display.Content{}, false, nil
	}
	p, ok := s.surprises.pop(kind, time.Now())
	return p.content, ok, nil
}

// RunSurprises fetches pictures ahead, one every few seconds, so the gallery does not refuse a burst.
func (s *Server) RunSurprises(ctx context.Context) {
	if s.Gallery == nil {
		return
	}
	s.surprises.mu.Lock()
	s.surprises.init()
	wake := s.surprises.wake
	s.surprises.mu.Unlock()
	for {
		for {
			kind, ok := s.surprises.short(time.Now())
			if !ok {
				break
			}
			if p, err := s.fetchSurprise(ctx, kind); err != nil {
				slog.Warn("no surprise picture", "kind", kind, "err", err)
			} else {
				s.surprises.keep(kind, p)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(cmp.Or(s.GalleryPace, surprisePace)):
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(10 * time.Minute):
		}
	}
}

func (s *Server) fetchSurprise(ctx context.Context, kind string) (surprise, error) {
	now := time.Now()
	label := "Aleatorio"
	var items []gallery.Item
	var err error
	if kind == Moment {
		var w display.Weather
		if s.Weather != nil {
			w = s.Weather.Reading().Weather
		}
		th := pickTheme(momentThemes(now, w))
		label = th.Label
		items, _, err = s.searchDivoom(ctx, th.Words[rand.IntN(len(th.Words))], 1)
		slices.SortFunc(items, func(a, b gallery.Item) int { return cmp.Compare(b.Likes, a.Likes) })
		items = items[:min(len(items), surpriseTop)]
	}
	if kind != Moment || err != nil || len(items) == 0 {
		category := gallery.Categories[randomCategories[rand.IntN(len(randomCategories))]]
		page := 1 + rand.IntN(8)
		items, err = s.listPage(ctx, category, gallery.MostLiked, page)
		if err == nil && len(items) == 0 && page > 1 {
			items, err = s.listPage(ctx, category, gallery.MostLiked, 1)
		}
		if err != nil {
			return surprise{}, err
		}
	}
	fresh := slices.DeleteFunc(slices.Clone(items), func(it gallery.Item) bool { return it.FileID == "" || s.surprises.seen(it.FileID) })
	if len(fresh) == 0 {
		fresh = items
	}
	if len(fresh) == 0 {
		return surprise{}, errors.New("the gallery gave nothing")
	}
	rand.Shuffle(len(fresh), func(i, j int) { fresh[i], fresh[j] = fresh[j], fresh[i] })
	// A file that will not decode is skipped for the next one, a few times.
	for _, it := range fresh[:min(len(fresh), 5)] {
		var b []byte
		var c display.Content
		if b, err = s.gifOf(ctx, it.FileID); err != nil {
			continue
		}
		if c, err = pictureOf(b, cmp.Or(strings.TrimSpace(it.Name), label)); err != nil {
			continue
		}
		c.Key = it.FileID
		s.surprises.remember(it.FileID)
		return surprise{content: c, item: it, theme: label, at: now}, nil
	}
	return surprise{}, err
}

func pickTheme(themes []theme) theme {
	total := 0
	for _, t := range themes {
		total += t.weight
	}
	n := rand.IntN(max(total, 1))
	for _, t := range themes {
		if n -= t.weight; n < 0 {
			return t
		}
	}
	return themes[len(themes)-1]
}

type surpriseRequest struct {
	Kind string `json:"kind"`
	For  string `json:"for"`
}

// surpriseShow shows a random picture, or one for the moment, now.
func (s *Server) surpriseShow(w http.ResponseWriter, r *http.Request) {
	var req surpriseRequest
	if !decode(w, r, &req, smallBody) {
		return
	}
	kind := map[string]string{"random": Random, "moment": Moment}[req.Kind]
	if kind == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("kind %q: want random or moment", req.Kind))
		return
	}
	d := defaultShow
	if req.For != "" {
		var err error
		if d, err = time.ParseDuration(req.For); err != nil || d <= 0 || d > maxAlert {
			writeError(w, http.StatusBadRequest, fmt.Errorf("for %q: want something like 30s, at most %s", req.For, maxAlert))
			return
		}
	}
	p, ok := s.surprises.pop(kind, time.Now())
	if !ok {
		var err error
		if p, err = s.fetchSurprise(r.Context(), kind); err != nil {
			writeError(w, galleryStatus(err), err)
			return
		}
	}
	s.Hub.Show(p.content, d)
	writeJSON(w, http.StatusAccepted, map[string]any{"showing": p.content.Name, "theme": p.theme, "file": p.item.FileID,
		"author": p.item.Author, "likes": p.item.Likes, "duration": d.String()})
}
