package server

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PlatanosVerdes/pixelhub/internal/atomicfile"
	"github.com/PlatanosVerdes/pixelhub/internal/display"
	"github.com/PlatanosVerdes/pixelhub/internal/gallery"
	"github.com/PlatanosVerdes/pixelhub/internal/lists"
	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
	"github.com/PlatanosVerdes/pixelhub/internal/state"
)

// crawlPasses are the pages of every category read for search: the most liked, and the newest,
// which have few likes yet and would otherwise never be found.
var crawlPasses = []struct {
	sort  gallery.Sort
	pages int
}{{gallery.MostLiked, 16}, {gallery.Newest, 8}}

// searchPages is how many pages of each category the crawl reads.
var searchPages = func() int {
	n := 0
	for _, p := range crawlPasses {
		n += p.pages
	}
	return n
}()

const (
	crawlPace    = 3 * time.Second
	reindexEvery = 6 * time.Hour
	listFresh    = 15 * time.Minute
	// The gallery answers at most about 30 pictures a request, so a page here is two of its pages.
	upstreamPage  = 30
	galleryPage   = 2 * upstreamPage
	galleryCached = 300
	defaultShow   = 30 * time.Second
)

type Gallery interface {
	List(ctx context.Context, category int, sort gallery.Sort, page, perPage int) ([]gallery.Item, error)
	Search(ctx context.Context, words string, page, perPage int) ([]gallery.Item, error)
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
	return s.cachedPage(fmt.Sprintf("%d/%d/%d", category, sort, page), func() ([]gallery.Item, error) {
		return s.Gallery.List(ctx, category, sort, page, upstreamPage)
	})
}

func (s *Server) searchPage(ctx context.Context, words string, page int) ([]gallery.Item, error) {
	return s.cachedPage(fmt.Sprintf("search/%d/%s", page, words), func() ([]gallery.Item, error) {
		return s.Gallery.Search(ctx, words, page, upstreamPage)
	})
}

// cachedPage answers a gallery page from memory while it is fresh, and puts what it fetches in the index.
func (s *Server) cachedPage(key string, fetch func() ([]gallery.Item, error)) ([]gallery.Item, error) {
	s.lists.mu.Lock()
	hit, ok := s.lists.pages[key]
	s.lists.mu.Unlock()
	if ok && time.Since(hit.at) < listFresh {
		return hit.items, nil
	}
	items, err := fetch()
	if err != nil {
		return nil, err
	}
	s.lists.mu.Lock()
	if s.lists.pages == nil {
		s.lists.pages = map[string]cachedPage{}
	}
	s.lists.pages[key] = cachedPage{items: items, at: time.Now()}
	s.lists.mu.Unlock()
	s.remember(items)
	return items, nil
}

// remember adds pictures to the search index, so whatever was browsed can be found again.
func (s *Server) remember(items []gallery.Item) {
	s.index.mu.Lock()
	defer s.index.mu.Unlock()
	if s.index.items == nil {
		s.index.items = map[string]gallery.Item{}
	}
	for _, it := range items {
		s.index.items[it.FileID] = it
	}
}

// galleryIndex is a slowly built list of the most liked and newest pages of every category, and of
// every page browsed, to search in:
// the gallery itself has no search without signing in, and answers 429 to bursts.
type galleryIndex struct {
	once  sync.Once
	mu    sync.Mutex
	items map[string]gallery.Item
	done  int
}

func (s *Server) indexGallery() {
	s.index.once.Do(func() {
		wait := s.loadIndex()
		go func() {
			time.Sleep(wait)
			s.crawl(context.Background())
		}()
	})
}

type savedIndex struct {
	At time.Time `json:"at"`
	// Pages is the searchPages it was built with; another number reads the gallery again at once.
	Pages int            `json:"pages"`
	Items []gallery.Item `json:"items"`
}

// loadIndex starts from the index saved last time, and says how long it stays fresh.
func (s *Server) loadIndex() time.Duration {
	if s.IndexPath == "" {
		return 0
	}
	data, err := os.ReadFile(s.IndexPath)
	var saved savedIndex
	if err != nil || json.Unmarshal(data, &saved) != nil {
		return 0
	}
	s.index.mu.Lock()
	defer s.index.mu.Unlock()
	s.index.items = map[string]gallery.Item{}
	for _, it := range saved.Items {
		s.index.items[it.FileID] = it
	}
	s.index.done = len(gallery.Categories) * searchPages
	if saved.Pages != searchPages {
		return 0
	}
	return max(reindexEvery-time.Since(saved.At), 0)
}

func (s *Server) saveIndex() {
	if s.IndexPath == "" {
		return
	}
	s.index.mu.Lock()
	saved := savedIndex{At: time.Now(), Pages: searchPages, Items: slices.Collect(maps.Values(s.index.items))}
	s.index.mu.Unlock()
	data, err := json.Marshal(saved)
	if err == nil {
		err = atomicfile.Write(s.IndexPath, data)
	}
	if err != nil {
		slog.Warn("gallery index not saved", "err", err)
	}
}

func (s *Server) crawl(ctx context.Context) {
	pace := cmp.Or(s.GalleryPace, crawlPace)
	categories := slices.Sorted(maps.Values(gallery.Categories))
	for {
		s.index.mu.Lock()
		rebuilding := s.index.done >= len(categories)*searchPages
		s.index.mu.Unlock()
		for _, c := range categories {
			for _, pass := range crawlPasses {
				for page := 1; page <= pass.pages; page++ {
					items, err := s.listPage(ctx, c, pass.sort, page)
					for errors.Is(err, gallery.ErrRateLimited) {
						time.Sleep(pace * 200)
						items, err = s.listPage(ctx, c, pass.sort, page)
					}
					s.index.mu.Lock()
					if !rebuilding {
						s.index.done++
					}
					s.index.mu.Unlock()
					time.Sleep(pace)
					if err == nil && len(items) == 0 {
						// The category ran out: count its remaining pages as read.
						s.index.mu.Lock()
						if !rebuilding {
							s.index.done += pass.pages - page
						}
						s.index.mu.Unlock()
						break
					}
				}
			}
		}
		s.index.mu.Lock()
		s.index.done = len(categories) * searchPages
		s.index.mu.Unlock()
		s.saveIndex()
		time.Sleep(reindexEvery)
	}
}

func (s *Server) gallerySearch(w http.ResponseWriter, r *http.Request) {
	words := strings.Fields(fold(r.URL.Query().Get("q")))
	if len(words) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("q: give some words to look for"))
		return
	}
	page, err := pageOf(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if items, more, err := s.searchDivoom(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")), page); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": itemsOf(items), "page": page, "more": more, "source": "divoom"})
		return
	} else {
		slog.Warn("gallery search falls back to the index", "err", err)
	}
	s.indexGallery()
	total := len(gallery.Categories) * searchPages
	s.index.mu.Lock()
	var found []gallery.Item
	for _, it := range s.index.items {
		if matches(it, words) {
			found = append(found, it)
		}
	}
	done := min(s.index.done, total)
	s.index.mu.Unlock()
	slices.SortFunc(found, func(a, b gallery.Item) int { return cmp.Or(b.Likes-a.Likes, strings.Compare(a.FileID, b.FileID)) })
	pages := max(1, (len(found)+galleryPage-1)/galleryPage)
	from := min(len(found), (page-1)*galleryPage)
	found = found[from:min(len(found), from+galleryPage)]
	writeJSON(w, http.StatusOK, map[string]any{"items": itemsOf(found), "page": page, "pages": pages, "more": page < pages, "indexed": done, "of": total, "source": "index"})
}

// searchDivoom asks the gallery's own search for a page, two of its pages long; more says whether
// it had a full page left to give.
func (s *Server) searchDivoom(ctx context.Context, words string, page int) ([]gallery.Item, bool, error) {
	var items []gallery.Item
	more := false
	for up := 2*page - 1; up <= 2*page; up++ {
		got, err := s.searchPage(ctx, words, up)
		if err != nil {
			return nil, false, err
		}
		items = append(items, got...)
		more = len(got) >= upstreamPage
		if !more {
			break
		}
	}
	seen := map[string]bool{}
	items = slices.DeleteFunc(items, func(it gallery.Item) bool {
		dup := seen[it.FileID]
		seen[it.FileID] = true
		return dup
	})
	return items, more, nil
}

// matches wants every word to start one of the item's words, so "cat" finds "cats" but not "vacation".
func matches(it gallery.Item, words []string) bool {
	have := strings.FieldsFunc(fold(it.Name+" "+it.Author+" "+it.About+" "+strings.Join(it.Tags, " ")), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	joined := strings.Join(have, "")
	for _, w := range words {
		prefix := slices.ContainsFunc(have, func(h string) bool { return strings.HasPrefix(h, w) })
		// A long word also finds the same words written apart: "mariobros" finds "Mario Bros".
		together := len(w) >= 6 && strings.Contains(joined, w)
		if !prefix && !together {
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
	mux.HandleFunc("POST /api/gallery/surprise", s.surpriseShow)
	if s.Lists != nil {
		s.listRoutes(mux)
	}
}

type galleryItem struct {
	File     string `json:"file"`
	Name     string `json:"name"`
	Author   string `json:"author"`
	Likes    int    `json:"likes"`
	About    string `json:"about,omitempty"`
	Category string `json:"category,omitempty"`
}

func itemsOf(items []gallery.Item) []galleryItem {
	out := make([]galleryItem, 0, len(items))
	for _, it := range items {
		out = append(out, galleryItem{File: it.FileID, Name: strings.TrimSpace(it.Name), Author: it.Author, Likes: it.Likes,
			About: strings.TrimSpace(it.About), Category: gallery.CategoryOf(it.Class)})
	}
	return out
}

func pageOf(r *http.Request) (int, error) {
	q := r.URL.Query().Get("page")
	if q == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(q)
	if err != nil || page < 1 || page > 100 {
		return 0, fmt.Errorf("page %q: want 1-100", q)
	}
	return page, nil
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
	page, err := pageOf(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var items []gallery.Item
	for up := 2*page - 1; up <= 2*page; up++ {
		got, err := s.listPage(r.Context(), category, sort, up)
		if err != nil {
			writeError(w, galleryStatus(err), err)
			return
		}
		items = append(items, got...)
	}
	seen := map[string]bool{}
	items = slices.DeleteFunc(items, func(it gallery.Item) bool {
		dup := seen[it.FileID]
		seen[it.FileID] = true
		return dup
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": itemsOf(items), "page": page, "more": len(items) > 0})
}

// gifOf fetches and decodes a gallery file once, then serves it from memory; a picture kept in a list
// comes from its own copy, so it works without the gallery.
func (s *Server) gifOf(ctx context.Context, file string) ([]byte, error) {
	if s.Lists != nil {
		if b, err := s.Lists.GIF(file); err == nil {
			return b, nil
		}
	}
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
	case errors.Is(err, gallery.ErrRateLimited):
		return http.StatusServiceUnavailable
	case errors.Is(err, gallery.ErrFormat):
		return http.StatusUnprocessableEntity
	case errors.Is(err, gallery.ErrBadFile):
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

type Lists interface {
	Lists() []lists.List
	Create(name string) error
	Rename(from, to string) error
	Delete(name string) error
	Add(list string, p lists.Picture, gif []byte) error
	Remove(list, file string) error
	GIF(file string) ([]byte, error)
}

func (s *Server) listRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/lists", func(w http.ResponseWriter, _ *http.Request) { s.replyLists(w, http.StatusOK, nil) })
	mux.HandleFunc("POST /api/lists", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if decode(w, r, &req, smallBody) {
			s.replyLists(w, http.StatusCreated, s.Lists.Create(req.Name))
		}
	})
	mux.HandleFunc("PUT /api/lists/{name}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &req, smallBody) {
			return
		}
		from := r.PathValue("name")
		err := s.Lists.Rename(from, req.Name)
		if err == nil {
			err = s.followLists(func(list string) string {
				if strings.EqualFold(list, from) {
					return strings.Join(strings.Fields(req.Name), " ")
				}
				return list
			})
		}
		s.replyLists(w, http.StatusOK, err)
	})
	mux.HandleFunc("DELETE /api/lists/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		err := s.Lists.Delete(name)
		if err == nil {
			err = s.followLists(func(list string) string {
				if strings.EqualFold(list, name) {
					return ""
				}
				return list
			})
		}
		s.replyLists(w, http.StatusOK, err)
	})
	mux.HandleFunc("POST /api/lists/{name}/upload", s.uploadToList)
	mux.HandleFunc("POST /api/lists/{name}/items", func(w http.ResponseWriter, r *http.Request) {
		var req galleryItem
		if !decode(w, r, &req, smallBody) {
			return
		}
		b, err := s.gifOf(r.Context(), req.File)
		if err != nil {
			writeError(w, galleryStatus(err), err)
			return
		}
		p := lists.Picture{File: req.File, Name: req.Name, Author: req.Author, Likes: req.Likes}
		s.replyLists(w, http.StatusCreated, s.Lists.Add(r.PathValue("name"), p, b))
	})
	mux.HandleFunc("DELETE /api/lists/{name}/items", func(w http.ResponseWriter, r *http.Request) {
		s.replyLists(w, http.StatusOK, s.Lists.Remove(r.PathValue("name"), r.URL.Query().Get("file")))
	})
}

func (s *Server) replyLists(w http.ResponseWriter, code int, err error) {
	switch {
	case errors.Is(err, lists.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, lists.ErrExists):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, lists.ErrInvalid):
		writeError(w, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, code, map[string]any{"lists": s.Lists.Lists()})
	}
}

// followLists keeps the lists named in the state in step with a renamed or deleted list; a scene
// left without its list is dropped.
func (s *Server) followLists(rename func(string) string) error {
	_, err := s.Hub.Update(func(st *state.State) error {
		for i := range st.Channels {
			c := &st.Channels[i]
			if c.Ambient.List != "" {
				c.Ambient.List = rename(c.Ambient.List)
			}
			c.Scenes = slices.DeleteFunc(c.Scenes, func(sc state.Scene) bool {
				if sc.List == "" {
					return false
				}
				sc.List = rename(sc.List)
				return sc.List == ""
			})
			for j := range c.Scenes {
				if c.Scenes[j].List != "" {
					c.Scenes[j].List = rename(c.Scenes[j].List)
				}
			}
		}
		return nil
	})
	return err
}

// uploadToList keeps a GIF or PNG of the user's own in a list, as a GIF like the gallery ones.
func (s *Server) uploadToList(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, sprite.MaxPictureBytes+uploadOverhead)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("want a multipart form with the picture in the field \"file\": %w", err))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read upload: %w", err))
		return
	}
	frames, err := sprite.DecodeBytes(data, s.Size)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("not a picture the display can show: %w", err))
		return
	}
	rgba := make([]*image.RGBA, 0, len(frames))
	for _, f := range frames {
		img := image.NewRGBA(f.Image.Bounds())
		draw.Draw(img, img.Rect, f.Image, f.Image.Bounds().Min, draw.Src)
		rgba = append(rgba, img)
	}
	b, err := gallery.GIF(rgba, frames[0].Delay)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	sum := sha256.Sum256(data)
	name := strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	p := lists.Picture{File: "upload/" + hex.EncodeToString(sum[:8]), Name: name}
	s.replyLists(w, http.StatusCreated, s.Lists.Add(r.PathValue("name"), p, b))
}

// WarmGallery fetches the first page the gallery tab opens on, pictures included, so it shows at once.
func (s *Server) WarmGallery(ctx context.Context) {
	if s.Gallery == nil {
		return
	}
	for up := 1; up <= 2; up++ {
		items, err := s.listPage(ctx, gallery.Categories["recommended"], gallery.MostLiked, up)
		if err != nil {
			slog.Warn("gallery not warmed", "err", err)
			return
		}
		for _, it := range items {
			if ctx.Err() != nil {
				return
			}
			_, _ = s.gifOf(ctx, it.FileID)
			time.Sleep(100 * time.Millisecond)
		}
	}
}
