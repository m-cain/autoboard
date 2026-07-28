package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/m-cain/autoboard/internal/app"
	"github.com/m-cain/autoboard/internal/httpapi"
)

func TestReadAPIProjectsBoardTicketAndNoWrites(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{
		Key:  "HTTP",
		Name: "HTTP project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	triage := createTicket(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Triage",
	})
	boardTicket := createTicket(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Board",
		Status:    app.TicketReady,
		Assignee:  app.AssigneeCodex,
	})
	canceledTicket := createTicket(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Canceled",
		Status:    app.TicketCanceled,
	})
	server := httptest.NewServer(httpapi.New(service, httpapi.Config{}))
	t.Cleanup(server.Close)

	var projects app.ProjectList
	getJSON(t, server.URL+"/api/v1/projects", &projects)
	if len(projects.Active) != 1 || projects.Active[0].Key != "HTTP" {
		t.Errorf("projects = %#v", projects)
	}
	var triageResponse struct {
		Tickets []app.Ticket `json:"tickets"`
	}
	getJSON(t, server.URL+"/api/v1/triage", &triageResponse)
	if len(triageResponse.Tickets) != 1 ||
		triageResponse.Tickets[0].Identifier != triage.Identifier {
		t.Errorf("triage = %#v", triageResponse)
	}
	var board app.ProjectBoard
	getJSON(t, server.URL+"/api/v1/projects/http/board", &board)
	if len(board.Columns.Ready) != 1 ||
		board.Columns.Ready[0].ID != boardTicket.ID {
		t.Errorf("board = %#v", board)
	}
	var detail app.TicketDetail
	getJSON(t, server.URL+"/api/v1/tickets/"+boardTicket.Identifier, &detail)
	if detail.ID != boardTicket.ID || detail.Project.ID != project.ID {
		t.Errorf("detail = %#v", detail)
	}
	var canceledResponse struct {
		Tickets []app.Ticket `json:"tickets"`
	}
	getJSON(
		t,
		server.URL+"/api/v1/projects/http/canceled",
		&canceledResponse,
	)
	if len(canceledResponse.Tickets) != 1 ||
		canceledResponse.Tickets[0].ID != canceledTicket.ID {
		t.Errorf("canceled = %#v", canceledResponse)
	}

	request, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/v1/projects",
		strings.NewReader(`{"key":"NOPE"}`),
	)
	if err != nil {
		t.Fatalf("create write request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("write request: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("POST status = %d, want 404", response.StatusCode)
	}
}

func TestReadAPIErrorsAndAttachmentDownload(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{
		Key:  "HTTP",
		Name: "HTTP project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket := createTicket(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Attached",
	})
	source := filepath.Join(t.TempDir(), "note ü.txt")
	if err := os.WriteFile(source, []byte("download me"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	attachment, _, err := service.AddAttachmentFromPath(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, ticket.ID, source)
	if err != nil {
		t.Fatalf("add attachment: %v", err)
	}
	server := httptest.NewServer(httpapi.New(service, httpapi.Config{}))
	t.Cleanup(server.Close)

	for _, fixture := range []struct {
		path   string
		status int
		kind   app.ErrorKind
	}{
		{
			path:   "/api/v1/projects/invalid!/board",
			status: http.StatusBadRequest,
			kind:   app.ErrorValidationFailed,
		},
		{
			path:   "/api/v1/tickets/HTTP-999",
			status: http.StatusNotFound,
			kind:   app.ErrorNotFound,
		},
		{
			path:   "/api/v1/attachments/not-a-uuid",
			status: http.StatusBadRequest,
			kind:   app.ErrorValidationFailed,
		},
		{
			path:   "/api/v1/projects/invalid!/canceled",
			status: http.StatusBadRequest,
			kind:   app.ErrorValidationFailed,
		},
		{
			path:   "/api/v1/not-a-route",
			status: http.StatusNotFound,
			kind:   app.ErrorNotFound,
		},
	} {
		response, err := http.Get(server.URL + fixture.path)
		if err != nil {
			t.Fatalf("GET %s: %v", fixture.path, err)
		}
		var envelope struct {
			Error struct {
				Kind app.ErrorKind `json:"kind"`
			} `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatalf("decode %s: %v", fixture.path, err)
		}
		response.Body.Close()
		if response.StatusCode != fixture.status || envelope.Error.Kind != fixture.kind {
			t.Errorf(
				"%s = status %d kind %q",
				fixture.path,
				response.StatusCode,
				envelope.Error.Kind,
			)
		}
	}

	response, err := http.Get(
		server.URL + "/api/v1/attachments/" + attachment.ID,
	)
	if err != nil {
		t.Fatalf("download attachment: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read attachment response: %v", err)
	}
	if response.StatusCode != http.StatusOK ||
		string(body) != "download me" ||
		!strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") ||
		!strings.Contains(response.Header.Get("Content-Disposition"), "filename*=") {
		t.Errorf("attachment response status=%d headers=%v body=%q", response.StatusCode, response.Header, body)
	}
}

func TestSSEReplaysActivityAndRejectsInvalidCursors(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{
		Key:  "SSE",
		Name: "SSE project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	server := httptest.NewServer(httpapi.New(service, httpapi.Config{
		PollInterval:      10 * time.Millisecond,
		HeartbeatInterval: time.Second,
	}))
	t.Cleanup(server.Close)

	request, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/v1/events?last_event_id=0",
		nil,
	)
	if err != nil {
		t.Fatalf("create SSE request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	reader := bufio.NewReader(response.Body)
	var lines []string
	for len(lines) < 4 {
		line, err := reader.ReadString('\n')
		if err != nil {
			response.Body.Close()
			t.Fatalf("read SSE: %v", err)
		}
		lines = append(lines, line)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != "text/event-stream" ||
		lines[0] != "id: 1\n" ||
		lines[1] != "event: activity\n" ||
		!strings.Contains(lines[2], `"project_id":"`+project.ID+`"`) ||
		lines[3] != "\n" {
		t.Errorf("SSE status=%d headers=%v lines=%q", response.StatusCode, response.Header, lines)
	}

	for _, cursor := range []string{"1.5", "-1", "999"} {
		response, err := http.Get(
			server.URL + "/api/v1/events?last_event_id=" + cursor,
		)
		if err != nil {
			t.Fatalf("invalid cursor %s: %v", cursor, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("cursor %s status = %d, want 400", cursor, response.StatusCode)
		}
	}
}

func TestReadAPIReportsUnavailableClosedService(t *testing.T) {
	root := t.TempDir()
	service, err := app.Open(context.Background(), app.Config{
		DatabasePath: filepath.Join(root, "autoboard.db"),
		DataDir:      filepath.Join(root, "data"),
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}
	server := httptest.NewServer(httpapi.New(service, httpapi.Config{}))
	t.Cleanup(server.Close)

	for _, fixture := range []struct {
		path   string
		status int
	}{
		{path: "/api/v1/projects", status: http.StatusInternalServerError},
		{path: "/api/v1/triage", status: http.StatusInternalServerError},
		{path: "/api/v1/projects/AUTO/board", status: http.StatusInternalServerError},
		{path: "/api/v1/projects/AUTO/canceled", status: http.StatusInternalServerError},
		{path: "/api/v1/tickets/AUTO-1", status: http.StatusInternalServerError},
		{path: "/api/v1/events", status: http.StatusServiceUnavailable},
		{path: "/health", status: http.StatusServiceUnavailable},
	} {
		response, err := http.Get(server.URL + fixture.path)
		if err != nil {
			t.Fatalf("GET %s: %v", fixture.path, err)
		}
		response.Body.Close()
		if response.StatusCode != fixture.status {
			t.Errorf(
				"%s status = %d, want %d",
				fixture.path,
				response.StatusCode,
				fixture.status,
			)
		}
		if fixture.status == http.StatusInternalServerError &&
			response.Header.Get("X-Request-ID") == "" {
			t.Errorf("%s missing correlation ID", fixture.path)
		}
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

func createTicket(
	t *testing.T,
	service *app.Service,
	input app.CreateTicketInput,
) app.Ticket {
	t.Helper()
	ticket, err := service.CreateTicket(context.Background(), app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, input)
	if err != nil {
		t.Fatalf("create ticket %q: %v", input.Title, err)
	}
	return ticket
}

func getJSON(t *testing.T, url string, target any) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status=%d body=%s", url, response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
