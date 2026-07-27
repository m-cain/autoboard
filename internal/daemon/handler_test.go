package daemon_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/m-cain/autoboard/internal/app"
	"github.com/m-cain/autoboard/internal/daemon"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOneHandlerServesHealthBrowserAPIAssetsAndMCP(t *testing.T) {
	root := t.TempDir()
	service, err := app.Open(context.Background(), app.Config{
		DatabasePath: filepath.Join(root, "autoboard.db"),
		DataDir:      filepath.Join(root, "data"),
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close service: %v", err)
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := httptest.NewUnstartedServer(daemon.NewHandler(
		service,
		fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<h1>Autoboard</h1>")},
		},
		daemon.Config{Address: listener.Addr().String()},
	))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	for _, path := range []string{"/health", "/api/v1/projects", "/projects"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.StatusCode, body)
		}
		if path == "/health" {
			var health struct {
				Status             string `json:"status"`
				SchemaVersion      int64  `json:"schema_version"`
				AttachmentWritable bool   `json:"attachment_writable"`
			}
			if err := json.Unmarshal(body, &health); err != nil {
				t.Fatalf("decode health: %v", err)
			}
			if health.Status != "ok" ||
				health.SchemaVersion != 1 ||
				!health.AttachmentWritable {
				t.Errorf("health = %#v", health)
			}
		}
	}

	client := mcp.NewClient(
		&mcp.Implementation{Name: "daemon-test", Version: "0.1.0"},
		nil,
	)
	session, err := client.Connect(
		context.Background(),
		&mcp.StreamableClientTransport{
			Endpoint:             server.URL + "/mcp",
			DisableStandaloneSSE: true,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close MCP: %v", err)
		}
	})
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_projects",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("list projects through MCP: %v", err)
	}
	if result.IsError {
		encoded, marshalErr := json.Marshal(result.Content)
		if marshalErr != nil {
			t.Fatalf("encode tool result: %v", marshalErr)
		}
		t.Fatalf("list projects tool error: %s", encoded)
	}
}

func TestHandlerBoundsMCPRequestBodies(t *testing.T) {
	service := openService(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := httptest.NewUnstartedServer(daemon.NewHandler(
		service,
		fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<h1>Autoboard</h1>")},
		},
		daemon.Config{Address: listener.Addr().String()},
	))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	request, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/mcp",
		strings.NewReader(strings.Repeat("x", (1<<20)+1)),
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 400 {
		t.Errorf("status = %d, want request rejection", response.StatusCode)
	}
}

func TestHandlerRejectsNonLoopbackPeersHostsAndOrigins(t *testing.T) {
	service := openService(t)
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>Autoboard</h1>")},
	}
	handler := daemon.NewHandler(
		service,
		assets,
		daemon.Config{Address: "127.0.0.1:4040"},
	)
	for _, fixture := range []struct {
		name       string
		path       string
		remoteAddr string
		host       string
		origin     string
	}{
		{
			name:       "remote peer",
			path:       "/api/v1/projects",
			remoteAddr: "192.0.2.10:2000",
			host:       "127.0.0.1:4040",
		},
		{
			name:       "foreign API host",
			path:       "/api/v1/projects",
			remoteAddr: "127.0.0.1:2000",
			host:       "attacker.example",
		},
		{
			name:       "foreign attachment host",
			path:       "/api/v1/attachments/00000000-0000-4000-8000-000000000000",
			remoteAddr: "127.0.0.1:2000",
			host:       "attacker.example",
		},
		{
			name:       "foreign SPA host",
			path:       "/projects",
			remoteAddr: "127.0.0.1:2000",
			host:       "attacker.example",
		},
		{
			name:       "foreign origin",
			path:       "/api/v1/projects",
			remoteAddr: "127.0.0.1:2000",
			host:       "127.0.0.1:4040",
			origin:     "https://attacker.example",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, fixture.path, nil)
			request.RemoteAddr = fixture.remoteAddr
			request.Host = fixture.host
			if fixture.origin != "" {
				request.Header.Set("Origin", fixture.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", response.Code)
			}
		})
	}
}

func TestHandlerPermitsOnlyConfiguredDevelopmentProxy(t *testing.T) {
	service := openService(t)
	handler := daemon.NewHandler(
		service,
		fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<h1>Autoboard</h1>")},
		},
		daemon.Config{
			Address:     "127.0.0.1:4040",
			Development: true,
		},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	request.RemoteAddr = "127.0.0.1:2000"
	request.Host = "localhost:5173"
	request.Header.Set("Origin", "http://localhost:5173")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", response.Code, response.Body)
	}
}

func openService(t *testing.T) *app.Service {
	t.Helper()
	root := t.TempDir()
	service, err := app.Open(context.Background(), app.Config{
		DatabasePath: filepath.Join(root, "autoboard.db"),
		DataDir:      filepath.Join(root, "data"),
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close service: %v", err)
		}
	})
	return service
}
