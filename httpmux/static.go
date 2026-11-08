package httpmux

import (
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/RogueTeam/xmrgateway/web"
)

// staticFiles serves dir when `xmrgateway deploy` has written it, and the
// embedded copy otherwise.
func staticFiles(dir string) http.Handler {
	var fsys fs.FS = web.Static()
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		fsys = os.DirFS(dir)
	}
	files := http.StripPrefix("/static/", http.FileServerFS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}
