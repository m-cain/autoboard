package mcpapi

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/m-cain/autoboard/internal/app"
)

type noInput struct{}

type projectRefInput struct {
	ProjectID string `json:"project_id"`
}

type ticketRefInput struct {
	TicketID string `json:"ticket_id"`
}

type searchTicketsInput struct {
	Query     string  `json:"query,omitempty"`
	ProjectID *string `json:"project_id,omitempty"`
	Limit     int     `json:"limit,omitempty"`
}

type actionableTicketsInput struct {
	ProjectID *string `json:"project_id,omitempty"`
	Limit     int     `json:"limit,omitempty"`
}

type attachmentRefInput struct {
	AttachmentID string `json:"attachment_id"`
}

type createProjectInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type updateProjectInput struct {
	ProjectID        string  `json:"project_id"`
	ExpectedRevision int     `json:"expected_revision"`
	Name             *string `json:"name,omitempty"`
	Description      *string `json:"description,omitempty"`
}

type projectRevisionInput struct {
	ProjectID        string `json:"project_id"`
	ExpectedRevision int    `json:"expected_revision"`
}

type createTicketInput struct {
	ProjectID      string   `json:"project_id"`
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Status         *string  `json:"status,omitempty"`
	Priority       *string  `json:"priority,omitempty"`
	Assignee       *string  `json:"assignee,omitempty"`
	ParentTicketID *string  `json:"parent_ticket_id,omitempty"`
	Labels         []string `json:"labels,omitempty"`
}

type updateTicketInput struct {
	TicketID         string    `json:"ticket_id"`
	ExpectedRevision int       `json:"expected_revision"`
	Title            *string   `json:"title,omitempty"`
	Description      *string   `json:"description,omitempty"`
	Priority         *string   `json:"priority,omitempty"`
	Assignee         *string   `json:"assignee,omitempty"`
	Labels           *[]string `json:"labels,omitempty"`
}

type transitionTicketInput struct {
	TicketID         string `json:"ticket_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Status           string `json:"status"`
}

type addCommentInput struct {
	TicketID string `json:"ticket_id"`
	Body     string `json:"body"`
}

type addAttachmentInput struct {
	TicketID string `json:"ticket_id"`
	Path     string `json:"path"`
}

type dependencyInput struct {
	BlockedTicketID  string `json:"blocked_ticket_id"`
	BlockerTicketID  string `json:"blocker_ticket_id"`
	ExpectedRevision int    `json:"expected_revision"`
}

type ticketListOutput struct {
	Tickets []app.Ticket `json:"tickets"`
}

type commentOutput struct {
	app.Comment
	TicketRevision int `json:"ticket_revision"`
}

type attachmentOutput struct {
	ID               string    `json:"id"`
	TicketID         string    `json:"ticket_id"`
	ProjectID        string    `json:"project_id"`
	OriginalFilename string    `json:"original_filename"`
	MediaType        string    `json:"media_type"`
	ByteSize         int64     `json:"byte_size"`
	SHA256           string    `json:"sha256"`
	Actor            app.Actor `json:"actor"`
	InsertedAt       string    `json:"inserted_at"`
	TicketRevision   int       `json:"ticket_revision,omitempty"`
	Content          *string   `json:"content,omitempty"`
	ManagedPath      *string   `json:"managed_path,omitempty"`
}

func schemaFor[T any](configure func(*jsonschema.Schema)) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	if configure != nil {
		configure(schema)
	}
	return schema
}

func noInputSchema() *jsonschema.Schema {
	return schemaFor[noInput](nil)
}

func projectRefSchema() *jsonschema.Schema {
	return schemaFor[projectRefInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "project_id")
	})
}

func ticketRefSchema() *jsonschema.Schema {
	return schemaFor[ticketRefInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "ticket_id")
	})
}

func searchTicketsSchema() *jsonschema.Schema {
	return schemaFor[searchTicketsInput](func(schema *jsonschema.Schema) {
		boundedString(schema, "query", 0, 500)
		setDefault(schema.Properties["query"], `""`)
		boundedReference(schema, "project_id")
		boundedInteger(schema, "limit", 100)
		setDefault(schema.Properties["limit"], "25")
	})
}

func actionableTicketsSchema() *jsonschema.Schema {
	return schemaFor[actionableTicketsInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "project_id")
		boundedInteger(schema, "limit", 100)
		setDefault(schema.Properties["limit"], "25")
	})
}

func attachmentRefSchema() *jsonschema.Schema {
	return schemaFor[attachmentRefInput](func(schema *jsonschema.Schema) {
		property := schema.Properties["attachment_id"]
		property.Pattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`
	})
}

func createProjectSchema() *jsonschema.Schema {
	return schemaFor[createProjectInput](func(schema *jsonschema.Schema) {
		key := schema.Properties["key"]
		key.Pattern = `^[A-Za-z][A-Za-z0-9]{1,7}$`
		boundedString(schema, "name", 1, 200)
		boundedString(schema, "description", 0, 100_000)
		setDefault(schema.Properties["description"], `""`)
	})
}

func updateProjectSchema() *jsonschema.Schema {
	return schemaFor[updateProjectInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "project_id")
		boundedInteger(schema, "expected_revision", 0)
		boundedString(schema, "name", 1, 200)
		boundedString(schema, "description", 0, 100_000)
		schema.AnyOf = []*jsonschema.Schema{
			{Required: []string{"name"}},
			{Required: []string{"description"}},
		}
	})
}

func projectRevisionSchema() *jsonschema.Schema {
	return schemaFor[projectRevisionInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "project_id")
		boundedInteger(schema, "expected_revision", 0)
	})
}

func createTicketSchema() *jsonschema.Schema {
	return schemaFor[createTicketInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "project_id")
		boundedString(schema, "title", 1, 500)
		boundedString(schema, "description", 0, 100_000)
		setDefault(schema.Properties["description"], `""`)
		enumString(schema, "status", ticketStatuses...)
		enumString(schema, "priority", priorities...)
		enumString(schema, "assignee", assignees...)
		boundedReference(schema, "parent_ticket_id")
		boundedLabels(schema)
	})
}

func updateTicketSchema() *jsonschema.Schema {
	return schemaFor[updateTicketInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "ticket_id")
		boundedInteger(schema, "expected_revision", 0)
		boundedString(schema, "title", 1, 500)
		boundedString(schema, "description", 0, 100_000)
		enumString(schema, "priority", priorities...)
		enumString(schema, "assignee", assignees...)
		boundedLabels(schema)
		schema.AnyOf = []*jsonschema.Schema{
			{Required: []string{"title"}},
			{Required: []string{"description"}},
			{Required: []string{"priority"}},
			{Required: []string{"assignee"}},
			{Required: []string{"labels"}},
		}
	})
}

func transitionTicketSchema() *jsonschema.Schema {
	return schemaFor[transitionTicketInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "ticket_id")
		boundedInteger(schema, "expected_revision", 0)
		enumString(schema, "status", ticketStatuses...)
	})
}

func addCommentSchema() *jsonschema.Schema {
	return schemaFor[addCommentInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "ticket_id")
		boundedString(schema, "body", 1, 100_000)
	})
}

func addAttachmentSchema() *jsonschema.Schema {
	return schemaFor[addAttachmentInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "ticket_id")
		path := schema.Properties["path"]
		path.Pattern = "^/"
		path.MaxLength = jsonschema.Ptr(4_096)
	})
}

func dependencySchema() *jsonschema.Schema {
	return schemaFor[dependencyInput](func(schema *jsonschema.Schema) {
		boundedReference(schema, "blocked_ticket_id")
		boundedReference(schema, "blocker_ticket_id")
		boundedInteger(schema, "expected_revision", 0)
	})
}

var (
	ticketStatuses = []string{
		"triage",
		"backlog",
		"ready",
		"in_progress",
		"done",
		"canceled",
	}
	priorities = []string{"none", "low", "medium", "high", "urgent"}
	assignees  = []string{"unassigned", "me", "codex"}
)

func boundedReference(schema *jsonschema.Schema, name string) {
	boundedString(schema, name, 1, 128)
}

func boundedString(
	schema *jsonschema.Schema,
	name string,
	minimum int,
	maximum int,
) {
	property := schema.Properties[name]
	if property == nil {
		return
	}
	property.MinLength = jsonschema.Ptr(minimum)
	property.MaxLength = jsonschema.Ptr(maximum)
}

func boundedInteger(
	schema *jsonschema.Schema,
	name string,
	maximum float64,
) {
	property := schema.Properties[name]
	if property == nil {
		return
	}
	property.Minimum = jsonschema.Ptr(1.0)
	if maximum > 0 {
		property.Maximum = jsonschema.Ptr(maximum)
	}
}

func enumString(schema *jsonschema.Schema, name string, values ...string) {
	property := schema.Properties[name]
	if property == nil {
		return
	}
	property.Enum = make([]any, len(values))
	for index, value := range values {
		property.Enum[index] = value
	}
}

func boundedLabels(schema *jsonschema.Schema) {
	property := schema.Properties["labels"]
	if property == nil {
		return
	}
	property.MaxItems = jsonschema.Ptr(100)
	if property.Items != nil {
		property.Items.MinLength = jsonschema.Ptr(1)
		property.Items.MaxLength = jsonschema.Ptr(100)
	}
}

func setDefault(schema *jsonschema.Schema, value string) {
	if schema != nil {
		schema.Default = json.RawMessage(value)
	}
}
