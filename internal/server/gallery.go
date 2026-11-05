package server

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/gallery"
)

const (
	searchPages   = 3
	searchWorkers = 6
	listFresh     = 15 * time.Minute
	galleryPage   = 24
	galleryCached = 300
	defaultShow   = 30 * time.Second
)

type Gallery interface {
	List(ctx context.Context, category int, sort gallery.Sort, page, perPage int) ([]gallery.Item, error)
	Download(ctx context.Context, fileID string) ([]byte, error)
}

// gifCache keeps decoded gallery files as GIFs; they never change once published.
type gifCache struct {
	mu    sync.Mutex
	order []string
	gifs  map[string][]byte
}

func (c *gifCache) get(id string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.gifs[id]
	return b, ok
}

func (c *gifCache) put(id string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gifs == nil {
		c.gifs = map[string][]byte{}
	}
	if _, ok := c.gifs[id]; ok {
		return
	}
	c.gifs[id] = b
	c.order = append(c.order, id)
	if len(c.order) > galleryCached {
		delete(c.gifs, c.order[0])
		c.order = c.order[1:]
	}
}

// listCache keeps gallery pages for a while, so searching does not refetch every category.
type listCache struct {
	mu    sync.Mutex
	pages map[string]cachedPage
}

type cachedPage struct {
	items []gallery.Item
	at    time.Time
}

func (s *Server) listPage(ctx context.Context, category int, sort gallery.Sort, page int) ([]gallery.Item, error) {
	key := fmt.Sprintf("%d/%d/%d", category, sort, page)
	s.lists.mu.Lock()
	hit, ok := s.lists.pages[key]
	s.lists.mu.Unlock()
	if ok && time.Since(hit.at) < listFresh {
		return hit.items, nil
	}
	items, err := s.Gallery.List(ctx, category, sort, page, galleryPage)
	if err != nil {
		return nil, err
	}
	s.lists.mu.Lock()
	if s.lists.pages == nil {
		s.lists.pages = map[string]cachedPage{}
	}
	s.lists.pages[key] = cachedPage{items: items, at: time.Now()}
	s.lists.mu.Unlock()
	return items, nil
}

// gallerySearch looks for words in the names, authors and tags of the most liked pages of every
// category: the gallery itself offers no search without signing in.
func (s *Server) gallerySearch(w http.ResponseWriter, r *http.Request) {
	words := strings.Fields(fold(r.URL.Query().Get("q")))
	if len(words) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("q: give some words to look for"))
		return
	}
	type job struct{ category, page int }
	jobs := make(chan job)
	var (
		mu    sync.Mutex
		found []gallery.Item
		seen  = map[string]bool{}
		wg    sync.WaitGroup
	)
	for range searchWorkers {
		wg.Go(func() {
			for j := range jobs {
				items, err := s.listPage(r.Context(), j.category, gallery.MostLiked, j.page)
				if err != nil {
					continue
				}
				mu.Lock()
				for _, it := range items {
					if !seen[it.FileID] && matches(it, words) {
						seen[it.FileID] = true
						found = append(found, it)
					}
				}
				mu.Unlock()
			}
		})
	}
	for _, c := range gallery.Categories {
		for page := 1; page <= searchPages; page++ {
			jobs <- job{c, page}
		}
	}
	close(jobs)
	wg.Wait()
	slices.SortFunc(found, func(a, b gallery.Item) int { return b.Likes - a.Likes })
	out := make([]galleryItem, 0, len(found))
	for _, it := range found {
		out = append(out, galleryItem{File: it.FileID, Name: it.Name, Author: it.Author, Likes: it.Likes})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "more": false})
}

// matches wants every word to start one of the item's words, so "cat" finds "cats" but not "vacation".
func matches(it gallery.Item, words []string) bool {
	have := strings.FieldsFunc(fold(it.Name+" "+it.Author+" "+strings.Join(it.Tags, " ")), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		if !slices.ContainsFunc(have, func(h string) bool { return strings.HasPrefix(h, w) }) {
			return false
		}
	}
	return true
}

var accents = strings.NewReplacer("á", "a", "à", "a", "ä", "a", "â", "a", "é", "e", "è", "e", "ë", "e", "ê", "e",
	"í", "i", "ì", "i", "ï", "i", "î", "i", "ó", "o", "ò", "o", "ö", "o", "ô", "o", "ú", "u", "ù", "u", "ü", "u", "û", "u", "ñ", "n", "ç", "c")

// fold lowercases and drops accents, so "corazón" finds "Corazon".
func fold(s string) string { return accents.Replace(strings.ToLower(s)) }

func (s *Server) galleryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/gallery/categories", func(w http.ResponseWriter, _ *http.Request) {
		names := make([]string, 0, len(gallery.Categories))
		for n := range gallery.Categories {
			names = append(names, n)
		}
		slices.Sort(names)
		writeJSON(w, http.StatusOK, map[string][]string{"categories": names})
	})
	mux.HandleFunc("GET /api/gallery", s.galleryList)
	mux.HandleFunc("GET /api/gallery/gif", s.galleryGIF)
	mux.HandleFunc("GET /api/gallery/search", s.gallerySearch)
	mux.HandleFunc("POST /api/gallery/show", s.galleryShow)
	mux.HandleFunc("POST /api/gallery/save", s.gallerySave)
}

type galleryItem struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	Author string `json:"author"`
	Likes  int    `json:"likes"`
}

func (s *Server) galleryList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	category, ok := gallery.Categories[q.Get("category")]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("category %q: see GET /api/gallery/categories", q.Get("category")))
		return
	}
	sort := gallery.MostLiked
	if q.Get("sort") == "new" {
		sort = gallery.Newest
	}
	page, err := strconv.Atoi(q.Get("page"))
	if q.Get("page") == "" {
		page, err = 1, nil
	}
	if err != nil || page < 1 || page > 100 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("page %q: want 1-100", q.Get("page")))
		return
	}
	items, err := s.listPage(r.Context(), category, sort, page)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]galleryItem, 0, len(items))
	for _, it := range items {
		out = append(out, galleryItem{File: it.FileID, Name: it.Name, Author: it.Author, Likes: it.Likes})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "more": len(items) > 0})
}

// gifOf fetches and decodes a gallery file once, then serves it from memory.
func (s *Server) gifOf(ctx context.Context, file string) ([]byte, error) {
	if b, ok := s.gifs.get(file); ok {
		return b, nil
	}
	raw, err := s.Gallery.Download(ctx, file)
	if err != nil {
		return nil, err
	}
	frames, delay, err := gallery.Decode(raw)
	if err != nil {
		return nil, err
	}
	b, err := gallery.GIF(frames, delay)
	if err != nil {
		return nil, err
	}
	s.gifs.put(file, b)
	return b, nil
}

func (s *Server) galleryGIF(w http.ResponseWriter, r *http.Request) {
	b, err := s.gifOf(r.Context(), r.URL.Query().Get("file"))
	if err != nil {
		writeError(w, galleryStatus(err), err)
		return
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(b)
}

type galleryPick struct {
	File    string `json:"file"`
	Name    string `json:"name"`
	For     string `json:"for"`
	Channel string `json:"channel"`
}

func (s *Server) galleryShow(w http.ResponseWriter, r *http.Request) {
	var req galleryPick
	if !decode(w, r, &req, smallBody) {
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
	b, err := s.gifOf(r.Context(), req.File)
	if err != nil {
		writeError(w, galleryStatus(err), err)
		return
	}
	content, err := pictureOf(b, "gallery "+cmp.Or(req.Name, "picture"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	s.Hub.Show(content, d)
	writeJSON(w, http.StatusAccepted, map[string]any{"showing": content.Name, "duration": d.String()})
}

func (s *Server) gallerySave(w http.ResponseWriter, r *http.Request) {
	var req galleryPick
	if !decode(w, r, &req, smallBody) {
		return
	}
	b, err := s.gifOf(r.Context(), req.File)
	if err != nil {
		writeError(w, galleryStatus(err), err)
		return
	}
	saved, err := s.Hub.AddPicture(req.Channel, cmp.Or(req.Name, "gallery")+".gif", b)
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"saved": saved})
}

func pictureOf(b []byte, name string) (display.Content, error) {
	g, err := gif.DecodeAll(bytes.NewReader(b))
	if err != nil {
		return display.Content{}, err
	}
	c := display.Content{Kind: display.Picture, Name: name}
	for i, frame := range g.Image {
		var img image.Image = frame
		c.Frames = append(c.Frames, display.Frame{Image: img, Delay: time.Duration(g.Delay[i]) * 10 * time.Millisecond})
	}
	return c, nil
}

func galleryStatus(err error) int {
	switch {
	case errors.Is(err, gallery.ErrFormat):
		return http.StatusUnprocessableEntity
	case errors.Is(err, gallery.ErrBadFile):
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}
