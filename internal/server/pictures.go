package server

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/sprite"
)

// started keeps a preview ETag from one run from matching a different picture in the next.
var started = time.Now().UnixNano()

const (
	previewScale = 10
	// Room for the multipart headers around a picture of the largest allowed size.
	uploadOverhead = 64 << 10
)

func (s *Server) listPictures(w http.ResponseWriter, r *http.Request) {
	files, err := s.Hub.ListPictures(r.PathValue("name"))
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"pictures": files})
}

// addPicture leaves the size check to the hub; the body limit only stops a far larger upload early.
func (s *Server) addPicture(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, sprite.MaxPictureBytes+uploadOverhead)
	file, header, err := r.FormFile("file")
	if err != nil {
		if tooBig := new(http.MaxBytesError); errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("%w: pictures are limited to %d bytes", sprite.ErrTooBig, sprite.MaxPictureBytes))
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Errorf("want a multipart form with the picture in the field \"file\": %w", err))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read upload: %w", err))
		return
	}
	saved, err := s.Hub.AddPicture(r.PathValue("name"), header.Filename, data)
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"file": saved})
}

// picturePNG draws the first frame of a picture like a sprite, so a UI can show thumbnails.
func (s *Server) picturePNG(w http.ResponseWriter, r *http.Request) {
	img, err := s.Hub.PictureImage(r.PathValue("name"), r.PathValue("file"))
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	writePNG(w, img)
}

func (s *Server) removePicture(w http.ResponseWriter, r *http.Request) {
	if err := s.Hub.RemovePicture(r.PathValue("name"), r.PathValue("file")); err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Sprites only change on a restart.
func (s *Server) spritePNG(w http.ResponseWriter, r *http.Request) {
	name, isPNG := strings.CutSuffix(r.PathValue("file"), ".png")
	c, ok := s.Sprites.Get(name)
	if !isPNG || !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no sprite %q, see GET /api/sprites", r.PathValue("file")))
		return
	}
	w.Header().Set("Cache-Control", "max-age=3600")
	writePNG(w, c.Frames[0].Image)
}

// previewPNG lets a polling client skip unchanged previews with If-None-Match.
func (s *Server) previewPNG(w http.ResponseWriter, r *http.Request) {
	c, version := s.Hub.Preview()
	etag := fmt.Sprintf(`"%d-%d"`, started, version)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writePNG(w, sprite.Render(c, s.Size))
}

func writePNG(w http.ResponseWriter, img image.Image) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, sprite.Resize(img, img.Bounds().Dx()*previewScale)); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("encode png: %w", err))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
}
