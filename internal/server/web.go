package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/PlatanosVerdes/pixelhub/web"
)

// withWeb leaves /api and /healthz to the API and answers every other path with a UI file, else index.html.
func (s *Server) withWeb(api http.Handler) http.Handler {
	ui := web.FS()
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			api.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if info, err := fs.Stat(ui, strings.TrimPrefix(r.URL.Path, "/")); err != nil || info.IsDir() {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
