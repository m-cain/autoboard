package webui_test

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/m-cain/autoboard/internal/webui"
)

type unavailableFS struct{}

func (unavailableFS) Open(string) (fs.File, error) {
	return nil, errors.New("asset storage unavailable")
}

func TestServesAssetsAndFallsBackToTheSPA(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data:    []byte("<main>Autoboard</main>"),
			ModTime: time.Unix(1, 0),
		},
		"assets/app-123.js": &fstest.MapFile{
			Data:    []byte("console.log('autoboard')"),
			ModTime: time.Unix(1, 0),
		},
	}
	server := httptest.NewServer(webui.New(assets))
	t.Cleanup(server.Close)

	for _, fixture := range []struct {
		path         string
		body         string
		contentType  string
		cacheControl string
	}{
		{
			path:         "/assets/app-123.js",
			body:         "console.log('autoboard')",
			contentType:  "text/javascript",
			cacheControl: "public, max-age=31536000, immutable",
		},
		{
			path:         "/projects/AUTO/tickets/AUTO-1",
			body:         "<main>Autoboard</main>",
			contentType:  "text/html",
			cacheControl: "no-cache",
		},
	} {
		response, err := http.Get(server.URL + fixture.path)
		if err != nil {
			t.Fatalf("GET %s: %v", fixture.path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", fixture.path, readErr)
		}
		if response.StatusCode != http.StatusOK ||
			string(body) != fixture.body ||
			!strings.HasPrefix(
				response.Header.Get("Content-Type"),
				fixture.contentType,
			) ||
			response.Header.Get("Cache-Control") != fixture.cacheControl {
			t.Errorf(
				"%s status=%d headers=%v body=%q",
				fixture.path,
				response.StatusCode,
				response.Header,
				body,
			)
		}
	}
}

func TestRejectsMissingFilesAndWrites(t *testing.T) {
	handler := webui.New(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("index")},
	})
	for _, fixture := range []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodGet, path: "/missing.js", status: http.StatusNotFound},
		{method: http.MethodPost, path: "/", status: http.StatusNotFound},
		{method: http.MethodGet, path: "/../secret", status: http.StatusNotFound},
	} {
		request := httptest.NewRequest(fixture.method, fixture.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != fixture.status {
			t.Errorf(
				"%s %s status = %d, want %d",
				fixture.method,
				fixture.path,
				response.Code,
				fixture.status,
			)
		}
	}
}

func TestReportsUnavailableOrUnbuiltAssets(t *testing.T) {
	for _, test := range []struct {
		name    string
		assets  fs.FS
		path    string
		message string
	}{
		{
			name:    "unavailable",
			assets:  unavailableFS{},
			path:    "/",
			message: "web assets unavailable",
		},
		{
			name:    "unbuilt",
			assets:  fstest.MapFS{},
			path:    "/projects/AUTO",
			message: "web application has not been built",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			webui.New(test.assets).ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable ||
				!strings.Contains(response.Body.String(), test.message) {
				t.Fatalf(
					"status=%d body=%q, want 503 containing %q",
					response.Code,
					response.Body.String(),
					test.message,
				)
			}
		})
	}
}

func TestEmbeddedAssetsExposeAPackagedFilesystem(t *testing.T) {
	if _, err := fs.ReadDir(webui.Assets(), "."); err != nil {
		t.Fatalf("read embedded asset root: %v", err)
	}
}
