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
	// Preload every asset once so serving a request is a map lookup instead
	// of a fresh copy of the embedded file bytes.
	files := make(map[string][]byte)
	err = fs.WalkDir(sub, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(sub, name)
		if err != nil {
			return err
		}
		files[name] = data
		return nil
	})
	if err != nil {
		panic(err)
	}
	return &handler{files: files}
}

type handler struct {
	files map[string][]byte
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
	data, ok := h.files[requestPath]
	if !ok && !strings.Contains(path.Base(requestPath), ".") {
		requestPath = "index.html"
		data, ok = h.files[requestPath]
	}
	if !ok {
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
