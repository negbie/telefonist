package telefonist

import (
	"io/fs"
	"net/http"

	"github.com/negbie/telefonist/assets"
)

// embeddedWebFS returns an fs.FS rooted at the embedded `web/` directory.
func embeddedWebFS() fs.FS {
	sub, err := fs.Sub(assets.WebAssets, "web")
	if err != nil {
		// This should never happen unless the embed directive is wrong.
		panic(err)
	}
	return sub
}

// noCacheFS wraps an http.FileSystem to set Cache-Control: no-store on all responses.
type noCacheFS struct {
	inner http.Handler
}

func (n *noCacheFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	n.inner.ServeHTTP(w, r)
}

// StaticHandler returns an http.Handler that serves the embedded web assets
// with no-cache headers. The "/" path serves index.html, and all other
// embedded files (JS, CSS) are served at their basename paths.
func StaticHandler() http.Handler {
	return &noCacheFS{inner: http.FileServer(http.FS(embeddedWebFS()))}
}
