package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return &handler{fs: sub}
}

type handler struct {
	fs fs.FS
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requestPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if requestPath == "" || requestPath == "." {
		requestPath = "index.html"
	}
	data, err := fs.ReadFile(h.fs, requestPath)
	if err != nil && !strings.Contains(path.Base(requestPath), ".") {
		requestPath = "index.html"
		data, err = fs.ReadFile(h.fs, requestPath)
	}
	if err != nil {
		if requestPath == "index.html" {
			http.Error(
				w,
				"WebUI assets are not built. Run `pnpm --dir web build`.",
				http.StatusServiceUnavailable,
			)
			return
		}
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(requestPath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if requestPath == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else if strings.Contains(path.Base(requestPath), "-") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeContent(w, r, requestPath, time.Time{}, bytes.NewReader(data))
}
