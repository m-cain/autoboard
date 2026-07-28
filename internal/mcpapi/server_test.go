package mcpapi_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
	"github.com/m-cain/autoboard/internal/mcpapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var toolNames = []string{
	"list_projects",
	"get_project_board",
	"search_tickets",
	"get_ticket",
	"list_actionable_tickets",
	"read_attachment",
	"create_project",
	"update_project",
	"archive_project",
	"restore_project",
	"create_ticket",
	"update_ticket",
	"transition_ticket",
	"add_comment",
	"add_attachment_from_path",
	"add_dependency",
	"remove_dependency",
}

func TestPublishesAllToolsWithStrictSchemasAndAnnotations(t *testing.T) {
	service := openService(t)
	session := connect(t, service)
	if session.ID() == "" {
		t.Fatal("Streamable HTTP session has no server-issued ID")
	}
	initialize := session.InitializeResult()
	if initialize == nil ||
		initialize.Instructions != mcpapi.Instructions ||
		initialize.ServerInfo == nil ||
		initialize.ServerInfo.Name != "autoboard" {
		t.Fatalf("initialize result = %#v", initialize)
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) != len(toolNames) {
		t.Fatalf("tool count = %d, want %d", len(listed.Tools), len(toolNames))
	}
	gotNames := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		gotNames = append(gotNames, tool.Name)
	}
	wantNames := slices.Clone(toolNames)
	slices.Sort(gotNames)
	slices.Sort(wantNames)
	if !slices.Equal(gotNames, wantNames) {
		t.Errorf("tool names = %q, want %q", gotNames, wantNames)
	}
	readOnlyTools := map[string]bool{
		"list_projects":           true,
		"get_project_board":       true,
		"search_tickets":          true,
		"get_ticket":              true,
		"list_actionable_tickets": true,
		"read_attachment":         true,
	}
	for _, tool := range listed.Tools {
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s input schema = %#v", tool.Name, tool.InputSchema)
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s permits additional properties: %#v", tool.Name, schema)
		}
		outputSchema, ok := tool.OutputSchema.(map[string]any)
		if !ok || outputSchema["additionalProperties"] != false {
			t.Errorf(
				"%s output schema is not strict: %#v",
				tool.Name,
				tool.OutputSchema,
			)
		}
		if tool.Annotations == nil {
			t.Fatalf("%s has no annotations", tool.Name)
		}
		if got := tool.Annotations.ReadOnlyHint; got != readOnlyTools[tool.Name] {
			t.Errorf("%s readOnlyHint = %v", tool.Name, got)
		}
		if tool.Annotations.OpenWorldHint == nil ||
			*tool.Annotations.OpenWorldHint {
			t.Errorf("%s openWorldHint = %#v, want false", tool.Name, tool.Annotations.OpenWorldHint)
		}
		wantDestructive := tool.Name == "archive_project" ||
			tool.Name == "remove_dependency"
		if tool.Annotations.DestructiveHint == nil ||
			*tool.Annotations.DestructiveHint != wantDestructive {
			t.Errorf(
				"%s destructiveHint = %#v, want %v",
				tool.Name,
				tool.Annotations.DestructiveHint,
				wantDestructive,
			)
		}
	}
}

func TestAllToolsRunOverStreamableHTTPWithStructuredOutput(t *testing.T) {
	service := openService(t)
	session := connect(t, service)
	ctx := context.Background()

	project := call[app.Project](t, ctx, session, "create_project", map[string]any{
		"key": "AUTO", "name": "Autoboard",
	})
	call[app.ProjectList](t, ctx, session, "list_projects", map[string]any{})
	project = call[app.Project](t, ctx, session, "update_project", map[string]any{
		"project_id": project.ID, "expected_revision": project.Revision,
		"description": "Go daemon",
	})
	first := call[app.Ticket](t, ctx, session, "create_ticket", map[string]any{
		"project_id": project.ID, "title": "First", "status": "ready",
		"priority": "urgent", "assignee": "codex", "labels": []string{"Backend"},
	})
	second := call[app.Ticket](t, ctx, session, "create_ticket", map[string]any{
		"project_id": project.ID, "title": "Second",
	})
	call[app.ProjectBoard](t, ctx, session, "get_project_board", map[string]any{
		"project_id": project.Key,
	})
	call[map[string]any](t, ctx, session, "search_tickets", map[string]any{
		"query": "First",
	})
	call[app.TicketDetail](t, ctx, session, "get_ticket", map[string]any{
		"ticket_id": first.Identifier,
	})
	call[map[string]any](t, ctx, session, "list_actionable_tickets", map[string]any{})
	first = call[app.Ticket](t, ctx, session, "update_ticket", map[string]any{
		"ticket_id": first.Identifier, "expected_revision": first.Revision,
		"title": "First updated",
	})
	comment := call[map[string]any](t, ctx, session, "add_comment", map[string]any{
		"ticket_id": first.ID, "body": "A note",
	})
	commentRevision, ok := comment["ticket_revision"].(float64)
	if !ok {
		t.Fatalf("comment ticket_revision = %#v", comment["ticket_revision"])
	}
	first.Revision = int(commentRevision)

	source := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write attachment source: %v", err)
	}
	attachment := call[map[string]any](
		t,
		ctx,
		session,
		"add_attachment_from_path",
		map[string]any{"ticket_id": first.ID, "path": source},
	)
	attachmentRevision, ok := attachment["ticket_revision"].(float64)
	if !ok {
		t.Fatalf(
			"attachment ticket_revision = %#v",
			attachment["ticket_revision"],
		)
	}
	first.Revision = int(attachmentRevision)
	call[map[string]any](t, ctx, session, "read_attachment", map[string]any{
		"attachment_id": attachment["id"],
	})

	first = call[app.Ticket](t, ctx, session, "add_dependency", map[string]any{
		"blocked_ticket_id": first.ID, "blocker_ticket_id": second.ID,
		"expected_revision": first.Revision,
	})
	first = call[app.Ticket](t, ctx, session, "remove_dependency", map[string]any{
		"blocked_ticket_id": first.ID, "blocker_ticket_id": second.ID,
		"expected_revision": first.Revision,
	})
	first = call[app.Ticket](t, ctx, session, "transition_ticket", map[string]any{
		"ticket_id": first.ID, "expected_revision": first.Revision, "status": "done",
	})
	if first.Status != app.TicketDone {
		t.Errorf("transitioned status = %q, want done", first.Status)
	}
	project = call[app.Project](t, ctx, session, "archive_project", map[string]any{
		"project_id": project.ID, "expected_revision": project.Revision,
	})
	call[app.Project](t, ctx, session, "restore_project", map[string]any{
		"project_id": project.ID, "expected_revision": project.Revision,
	})
}

func TestToolErrorsAreRepairableAndInvalidInputNeverMutates(t *testing.T) {
	service := openService(t)
	session := connect(t, service)
	ctx := context.Background()
	project := call[app.Project](t, ctx, session, "create_project", map[string]any{
		"key": "AUTO", "name": "Autoboard",
	})
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "update_project",
		Arguments: map[string]any{
			"project_id": project.ID, "expected_revision": 99, "name": "Stale", "initiated_by": "me",
		},
	})
	if err != nil {
		t.Fatalf("call stale update: %v", err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("stale result = %#v", result)
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("stale error content = %#v", result.Content[0])
	}
	text := textContent.Text
	if !containsAll(text, "revision_conflict", "Repair:", "Current entity:") {
		t.Errorf("stale error text = %q", text)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_project",
		Arguments: map[string]any{"key": "x", "name": "Bad", "sql": "nope"},
	})
	if err != nil {
		t.Fatalf("call invalid create: %v", err)
	}
	if !result.IsError {
		t.Errorf("invalid input result = %#v, want tool error", result)
	}
	projects, err := service.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects.Active) != 1 {
		t.Errorf("project count = %d, want 1", len(projects.Active))
	}
}

func TestEveryWriteToolRejectsInvalidInitiatorWithoutMutation(t *testing.T) {
	service := openService(t)
	session := connect(t, service)
	ctx := context.Background()
	attr := app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalMe}
	project, err := service.CreateProject(ctx, attr, app.CreateProjectInput{Key: "BASE", Name: "Base"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateTicket(ctx, attr, app.CreateTicketInput{ProjectID: project.ID, Title: "First", Status: app.TicketReady})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateTicket(ctx, attr, app.CreateTicketInput{ProjectID: project.ID, Title: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := service.CreateProject(ctx, attr, app.CreateProjectInput{Key: "ARCH", Name: "Archived"})
	if err != nil {
		t.Fatal(err)
	}
	archived, err = service.ArchiveProject(ctx, attr, archived.ID, archived.Revision)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(source, []byte("note"), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := map[string]map[string]any{
		"create_project":           {"key": "NEWP", "name": "New"},
		"update_project":           {"project_id": project.ID, "expected_revision": project.Revision, "name": "Renamed"},
		"archive_project":          {"project_id": project.ID, "expected_revision": project.Revision},
		"restore_project":          {"project_id": archived.ID, "expected_revision": archived.Revision},
		"create_ticket":            {"project_id": project.ID, "title": "Created"},
		"update_ticket":            {"ticket_id": first.ID, "expected_revision": first.Revision, "title": "Updated"},
		"transition_ticket":        {"ticket_id": first.ID, "expected_revision": first.Revision, "status": "done"},
		"add_comment":              {"ticket_id": first.ID, "body": "Comment"},
		"add_attachment_from_path": {"ticket_id": first.ID, "path": source},
		"add_dependency":           {"blocked_ticket_id": first.ID, "blocker_ticket_id": second.ID, "expected_revision": first.Revision},
		"remove_dependency":        {"blocked_ticket_id": first.ID, "blocker_ticket_id": second.ID, "expected_revision": first.Revision},
	}
	for name, valid := range arguments {
		before, err := mutationSnapshot(t, service, ctx, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, invalid := range []map[string]any{
			{}, {"initiated_by": "system"}, {"initiated_by": "unknown"}, {"initiated_by": 1},
		} {
			args := maps.Clone(valid)
			maps.Copy(args, invalid)
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				t.Fatalf("%s call: %v", name, err)
			}
			if !result.IsError {
				t.Errorf("%s accepted invalid initiator %#v", name, invalid)
			}
			after, err := mutationSnapshot(t, service, ctx, first.ID)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Errorf("%s mutated state for %#v", name, invalid)
			}
		}
	}
}

func mutationSnapshot(t *testing.T, service *app.Service, ctx context.Context, id string) ([]byte, error) {
	t.Helper()
	return json.Marshal(struct {
		Projects app.ProjectList     `json:"projects"`
		Ticket   app.TicketDetail    `json:"ticket"`
		Activity []app.ActivityEvent `json:"activity"`
	}{mustProjects(t, service, ctx), mustTicket(t, service, ctx, id), mustActivity(t, service, ctx)})
}

func mustProjects(t *testing.T, service *app.Service, ctx context.Context) app.ProjectList {
	t.Helper()
	value, err := service.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustTicket(t *testing.T, service *app.Service, ctx context.Context, id string) app.TicketDetail {
	t.Helper()
	value, err := service.GetTicketDetail(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustActivity(t *testing.T, service *app.Service, ctx context.Context) []app.ActivityEvent {
	t.Helper()
	value, err := service.ListActivityAfter(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	return value
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

func connect(t *testing.T, service *app.Service) *mcp.ClientSession {
	t.Helper()
	server := httptest.NewServer(mcpapi.New(service))
	t.Cleanup(server.Close)
	client := mcp.NewClient(
		&mcp.Implementation{Name: "autoboard-test", Version: "0.1.0"},
		nil,
	)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             server.URL,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close MCP session: %v", err)
		}
	})
	return session
}

func call[T any](
	t *testing.T,
	ctx context.Context,
	session *mcp.ClientSession,
	name string,
	arguments map[string]any,
) T {
	t.Helper()
	if map[string]bool{
		"create_project": true, "update_project": true, "archive_project": true,
		"restore_project": true, "create_ticket": true, "update_ticket": true,
		"transition_ticket": true, "add_comment": true, "add_attachment_from_path": true,
		"add_dependency": true, "remove_dependency": true,
	}[name] {
		arguments["initiated_by"] = "me"
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: name, Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("call %s tool error: %#v", name, result.Content)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encode %s output: %v", name, err)
	}
	var output T
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatalf("decode %s output: %v", name, err)
	}
	return output
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
