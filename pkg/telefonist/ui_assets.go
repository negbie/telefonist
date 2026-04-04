package telefonist

import (
	"io/fs"
	"log"
	"net/http"

	"github.com/negbie/telefonist/assets"
)

var webFS = mustSubFS(assets.WebAssets, "web")

func mustSubFS(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		log.Printf("failed to create sub filesystem for %q: %v", dir, err)
		return fsys
	}
	return sub
}

type noCacheFS struct {
	inner http.Handler
}

func (n *noCacheFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	n.inner.ServeHTTP(w, r)
}

func StaticHandler() http.Handler {
	return &noCacheFS{inner: http.FileServer(http.FS(webFS))}
}
