package webui

import (
	"bytes"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

type handler struct {
	assets fs.FS
}

func New(assets fs.FS) http.Handler {
	return &handler{assets: assets}
}

func (h *handler) ServeHTTP(
	response http.ResponseWriter,
	request *http.Request,
) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.NotFound(response, request)
		return
	}
	if invalidPath(request.URL.Path) {
		http.NotFound(response, request)
		return
	}

	name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	info, err := fs.Stat(h.assets, name)
	if err == nil && info.Mode().IsRegular() {
		h.serveFile(response, request, name, info.ModTime())
		return
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		http.Error(response, "web assets unavailable", http.StatusServiceUnavailable)
		return
	}
	if path.Ext(name) != "" {
		http.NotFound(response, request)
		return
	}
	index, err := fs.Stat(h.assets, "index.html")
	if err != nil || !index.Mode().IsRegular() {
		http.Error(
			response,
			"web application has not been built",
			http.StatusServiceUnavailable,
		)
		return
	}
	h.serveFile(response, request, "index.html", index.ModTime())
}

func (h *handler) serveFile(
	response http.ResponseWriter,
	request *http.Request,
	name string,
	modified time.Time,
) {
	content, err := fs.ReadFile(h.assets, name)
	if err != nil {
		http.Error(response, "web assets unavailable", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set(
		"Content-Security-Policy",
		"default-src 'self'; connect-src 'self'; img-src 'self' data:; "+
			"style-src 'self'; script-src 'self'; object-src 'none'; "+
			"base-uri 'none'; frame-ancestors 'none'",
	)
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	if strings.HasPrefix(name, "assets/") {
		response.Header().Set(
			"Cache-Control",
			"public, max-age=31536000, immutable",
		)
	} else {
		response.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(
		response,
		request,
		path.Base(name),
		modified,
		bytes.NewReader(content),
	)
}

func invalidPath(value string) bool {
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}
