package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/m-cain/autoboard/internal/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Instructions = "Autoboard is a direct-write project board. Tickets assigned to `me` are reserved for the human. Execute only tickets returned by list_actionable_tickets unless the human explicitly instructs otherwise. Read the latest entity before revision-checked writes. Confirm broad reorganizations, project archival, and dependency removal with the human."

type registry struct {
	service *app.Service
	server  *mcp.Server
}

func New(service *app.Service) http.Handler {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "autoboard", Version: "0.1.0"},
		&mcp.ServerOptions{
			Instructions: Instructions,
			Capabilities: &mcp.ServerCapabilities{},
		},
	)
	registry := &registry{service: service, server: server}
	registry.registerReadTools()
	registry.registerWriteTools()
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			JSONResponse:          true,
			CrossOriginProtection: http.NewCrossOriginProtection(),
			SessionTimeout:        30 * time.Minute,
		},
	)
}

func (r *registry) registerReadTools() {
	addTool(
		r.server,
		readTool(
			"list_projects",
			"List active and archived Autoboard projects.",
			noInputSchema(),
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (
			*mcp.CallToolResult,
			app.ProjectList,
			error,
		) {
			output, err := r.service.ListProjects(ctx)
			if err != nil {
				return failure[app.ProjectList](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		readTool(
			"get_project_board",
			"Read a project's Kanban board and project metadata.",
			projectRefSchema(),
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input projectRefInput) (
			*mcp.CallToolResult,
			app.ProjectBoard,
			error,
		) {
			output, err := r.service.GetProjectBoard(ctx, input.ProjectID)
			if err != nil {
				return failure[app.ProjectBoard](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		readTool(
			"search_tickets",
			"Search tickets globally or within one project.",
			searchTicketsSchema(),
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input searchTicketsInput) (
			*mcp.CallToolResult,
			ticketListOutput,
			error,
		) {
			if input.Limit == 0 {
				input.Limit = 25
			}
			projectID, err := r.optionalProjectID(ctx, input.ProjectID)
			if err != nil {
				return failure[ticketListOutput](err)
			}
			tickets, err := r.service.SearchTickets(
				ctx,
				input.Query,
				projectID,
				input.Limit,
			)
			if err != nil {
				return failure[ticketListOutput](err)
			}
			return nil, ticketListOutput{Tickets: tickets}, nil
		},
	)
	addTool(
		r.server,
		readTool(
			"get_ticket",
			"Read complete ticket detail, including relationships, comments, attachments, and activity.",
			ticketRefSchema(),
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input ticketRefInput) (
			*mcp.CallToolResult,
			app.TicketDetail,
			error,
		) {
			output, err := r.service.GetTicketDetail(ctx, input.TicketID)
			if err != nil {
				return failure[app.TicketDetail](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		readTool(
			"list_actionable_tickets",
			"List ready, unblocked tickets assigned to codex; tickets assigned to `me` and tickets with non-terminal subtasks are excluded.",
			actionableTicketsSchema(),
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input actionableTicketsInput) (
			*mcp.CallToolResult,
			ticketListOutput,
			error,
		) {
			if input.Limit == 0 {
				input.Limit = 25
			}
			projectID, err := r.optionalProjectID(ctx, input.ProjectID)
			if err != nil {
				return failure[ticketListOutput](err)
			}
			tickets, err := r.service.ListActionableTickets(
				ctx,
				projectID,
				input.Limit,
			)
			if err != nil {
				return failure[ticketListOutput](err)
			}
			return nil, ticketListOutput{Tickets: tickets}, nil
		},
	)
	addTool(
		r.server,
		readTool(
			"read_attachment",
			"Read a managed attachment's inline UTF-8 text when available, otherwise return its local managed path and metadata.",
			attachmentRefSchema(),
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input attachmentRefInput) (
			*mcp.CallToolResult,
			attachmentOutput,
			error,
		) {
			read, err := r.service.ReadAttachment(ctx, input.AttachmentID)
			if err != nil {
				return failure[attachmentOutput](err)
			}
			output := presentAttachment(read.Attachment)
			output.Content = read.Content
			output.ManagedPath = read.ManagedPath
			text := fmt.Sprintf(
				"Attachment %s is not returned inline. Inspect the managed local path: %s.",
				output.OriginalFilename,
				stringValue(output.ManagedPath),
			)
			if output.Content != nil {
				text = fmt.Sprintf(
					"Attachment %s (inline UTF-8 content):\n\n%s",
					output.OriginalFilename,
					*output.Content,
				)
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: text}},
			}, output, nil
		},
	)
}

func (r *registry) registerWriteTools() {
	addTool(
		r.server,
		writeTool(
			"create_project",
			"Create an active project with an immutable key.",
			createProjectSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input createProjectInput) (
			*mcp.CallToolResult,
			app.Project,
			error,
		) {
			output, err := r.service.CreateProject(ctx, app.CreateProjectInput{
				Key: input.Key, Name: input.Name, Description: input.Description,
			})
			if err != nil {
				return failure[app.Project](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"update_project",
			"Update a project's name or Markdown description using its current revision.",
			updateProjectSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input updateProjectInput) (
			*mcp.CallToolResult,
			app.Project,
			error,
		) {
			project, err := r.service.GetProject(ctx, input.ProjectID)
			if err != nil {
				return failure[app.Project](err)
			}
			output, err := r.service.UpdateProject(
				ctx,
				project.ID,
				input.ExpectedRevision,
				app.UpdateProjectInput{
					Name: input.Name, Description: input.Description,
				},
			)
			if err != nil {
				return failure[app.Project](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"archive_project",
			"Archive a project using its current revision. Confirm with the human before archival.",
			projectRevisionSchema(),
			true,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input projectRevisionInput) (
			*mcp.CallToolResult,
			app.Project,
			error,
		) {
			project, err := r.service.GetProject(ctx, input.ProjectID)
			if err != nil {
				return failure[app.Project](err)
			}
			output, err := r.service.ArchiveProject(
				ctx,
				project.ID,
				input.ExpectedRevision,
			)
			if err != nil {
				return failure[app.Project](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"restore_project",
			"Restore an archived project using its current revision.",
			projectRevisionSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input projectRevisionInput) (
			*mcp.CallToolResult,
			app.Project,
			error,
		) {
			project, err := r.service.GetProject(ctx, input.ProjectID)
			if err != nil {
				return failure[app.Project](err)
			}
			output, err := r.service.RestoreProject(
				ctx,
				project.ID,
				input.ExpectedRevision,
			)
			if err != nil {
				return failure[app.Project](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"create_ticket",
			"Create a ticket, optionally as a one-level subtask, in an active project.",
			createTicketSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input createTicketInput) (
			*mcp.CallToolResult,
			app.Ticket,
			error,
		) {
			project, err := r.service.GetProject(ctx, input.ProjectID)
			if err != nil {
				return failure[app.Ticket](err)
			}
			parentID := input.ParentTicketID
			if parentID != nil {
				resolved, err := r.service.ResolveTicketID(ctx, *parentID)
				if err != nil {
					return failure[app.Ticket](err)
				}
				parentID = &resolved
			}
			status := app.TicketStatus("")
			if input.Status != nil {
				status = app.TicketStatus(*input.Status)
			}
			priority := app.Priority("")
			if input.Priority != nil {
				priority = app.Priority(*input.Priority)
			}
			assignee := app.Assignee("")
			if input.Assignee != nil {
				assignee = app.Assignee(*input.Assignee)
			}
			output, err := r.service.CreateTicket(ctx, app.CreateTicketInput{
				ProjectID:      project.ID,
				Title:          input.Title,
				Description:    input.Description,
				Status:         status,
				Priority:       priority,
				Assignee:       assignee,
				ParentTicketID: parentID,
				Labels:         input.Labels,
			})
			if err != nil {
				return failure[app.Ticket](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"update_ticket",
			"Update ticket fields and labels using its current revision.",
			updateTicketSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input updateTicketInput) (
			*mcp.CallToolResult,
			app.Ticket,
			error,
		) {
			ticketID, err := r.service.ResolveTicketID(ctx, input.TicketID)
			if err != nil {
				return failure[app.Ticket](err)
			}
			output, err := r.service.UpdateTicket(
				ctx,
				ticketID,
				input.ExpectedRevision,
				app.UpdateTicketInput{
					Title:       input.Title,
					Description: input.Description,
					Priority:    priorityPointer(input.Priority),
					Assignee:    assigneePointer(input.Assignee),
					Labels:      input.Labels,
				},
			)
			if err != nil {
				return failure[app.Ticket](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"transition_ticket",
			"Move a ticket to a status using its current revision.",
			transitionTicketSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input transitionTicketInput) (
			*mcp.CallToolResult,
			app.Ticket,
			error,
		) {
			ticketID, err := r.service.ResolveTicketID(ctx, input.TicketID)
			if err != nil {
				return failure[app.Ticket](err)
			}
			output, err := r.service.TransitionTicket(
				ctx,
				ticketID,
				input.ExpectedRevision,
				app.TicketStatus(input.Status),
			)
			if err != nil {
				return failure[app.Ticket](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"add_comment",
			"Append a Markdown comment to a ticket.",
			addCommentSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input addCommentInput) (
			*mcp.CallToolResult,
			commentOutput,
			error,
		) {
			ticketID, err := r.service.ResolveTicketID(ctx, input.TicketID)
			if err != nil {
				return failure[commentOutput](err)
			}
			comment, ticket, err := r.service.AddComment(
				ctx,
				ticketID,
				input.Body,
			)
			if err != nil {
				return failure[commentOutput](err)
			}
			return nil, commentOutput{
				Comment: comment, TicketRevision: ticket.Revision,
			}, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"add_attachment_from_path",
			"Copy an absolute local file into managed attachment storage for a ticket.",
			addAttachmentSchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input addAttachmentInput) (
			*mcp.CallToolResult,
			attachmentOutput,
			error,
		) {
			ticketID, err := r.service.ResolveTicketID(ctx, input.TicketID)
			if err != nil {
				return failure[attachmentOutput](err)
			}
			attachment, ticket, err := r.service.AddAttachmentFromPath(
				ctx,
				ticketID,
				input.Path,
			)
			if err != nil {
				return failure[attachmentOutput](err)
			}
			output := presentAttachment(attachment)
			output.TicketRevision = ticket.Revision
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"add_dependency",
			"Add a same-project blocker dependency using the blocked ticket's current revision.",
			dependencySchema(),
			false,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input dependencyInput) (
			*mcp.CallToolResult,
			app.Ticket,
			error,
		) {
			output, err := r.mutateDependency(ctx, input, true)
			if err != nil {
				return failure[app.Ticket](err)
			}
			return nil, output, nil
		},
	)
	addTool(
		r.server,
		writeTool(
			"remove_dependency",
			"Remove a blocker dependency using the blocked ticket's current revision. Confirm with the human first.",
			dependencySchema(),
			true,
		),
		func(ctx context.Context, _ *mcp.CallToolRequest, input dependencyInput) (
			*mcp.CallToolResult,
			app.Ticket,
			error,
		) {
			output, err := r.mutateDependency(ctx, input, false)
			if err != nil {
				return failure[app.Ticket](err)
			}
			return nil, output, nil
		},
	)
}

func (r *registry) optionalProjectID(
	ctx context.Context,
	projectRef *string,
) (string, error) {
	if projectRef == nil {
		return "", nil
	}
	project, err := r.service.GetProject(ctx, *projectRef)
	if err != nil {
		return "", err
	}
	return project.ID, nil
}

func (r *registry) mutateDependency(
	ctx context.Context,
	input dependencyInput,
	add bool,
) (app.Ticket, error) {
	blockedID, err := r.service.ResolveTicketID(ctx, input.BlockedTicketID)
	if err != nil {
		return app.Ticket{}, err
	}
	blockerID, err := r.service.ResolveTicketID(ctx, input.BlockerTicketID)
	if err != nil {
		return app.Ticket{}, err
	}
	if add {
		return r.service.AddDependency(
			ctx,
			blockedID,
			blockerID,
			input.ExpectedRevision,
		)
	}
	return r.service.RemoveDependency(
		ctx,
		blockedID,
		blockerID,
		input.ExpectedRevision,
	)
}

func addTool[Input, Output any](
	server *mcp.Server,
	tool *mcp.Tool,
	handler mcp.ToolHandlerFor[Input, Output],
) {
	inputSchema, ok := tool.InputSchema.(*jsonschema.Schema)
	if !ok {
		panic(fmt.Sprintf("tool %q input schema is not a JSON schema", tool.Name))
	}
	inputResolved, err := inputSchema.Resolve(
		&jsonschema.ResolveOptions{ValidateDefaults: true},
	)
	if err != nil {
		panic(fmt.Sprintf("resolve tool %q input schema: %v", tool.Name, err))
	}
	outputSchema, err := jsonschema.For[Output](nil)
	if err != nil {
		panic(fmt.Sprintf("derive tool %q output schema: %v", tool.Name, err))
	}
	outputResolved, err := outputSchema.Resolve(nil)
	if err != nil {
		panic(fmt.Sprintf("resolve tool %q output schema: %v", tool.Name, err))
	}
	tool.OutputSchema = outputSchema

	server.AddTool(tool, func(
		ctx context.Context,
		request *mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		arguments := make(map[string]any)
		if request != nil &&
			request.Params != nil &&
			len(request.Params.Arguments) > 0 {
			if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil {
				return validationFailure(
					fmt.Errorf("decode tool arguments: %w", err),
				), nil
			}
		}
		if err := inputResolved.ApplyDefaults(&arguments); err != nil {
			return validationFailure(
				fmt.Errorf("apply argument defaults: %w", err),
			), nil
		}
		if err := inputResolved.Validate(arguments); err != nil {
			return validationFailure(err), nil
		}
		encodedArguments, err := json.Marshal(arguments)
		if err != nil {
			return nil, fmt.Errorf("encode validated tool arguments: %w", err)
		}
		var input Input
		if err := json.Unmarshal(encodedArguments, &input); err != nil {
			return validationFailure(
				fmt.Errorf("decode validated tool arguments: %w", err),
			), nil
		}

		result, output, handlerErr := handler(ctx, request, input)
		if handlerErr != nil {
			if result == nil {
				result = toolFailure(handlerErr)
			}
			return result, nil
		}
		if result == nil {
			result = &mcp.CallToolResult{}
		}
		encodedOutput, err := json.Marshal(output)
		if err != nil {
			return nil, fmt.Errorf("encode tool output: %w", err)
		}
		var outputValue any
		if err := json.Unmarshal(encodedOutput, &outputValue); err != nil {
			return nil, fmt.Errorf("decode tool output for validation: %w", err)
		}
		if err := outputResolved.Validate(outputValue); err != nil {
			return nil, fmt.Errorf("validate tool output: %w", err)
		}
		result.StructuredContent = json.RawMessage(encodedOutput)
		if result.Content == nil {
			result.Content = []mcp.Content{
				&mcp.TextContent{Text: string(encodedOutput)},
			}
		}
		return result, nil
	})
}

func readTool(name string, description string, schema any) *mcp.Tool {
	return tool(name, description, schema, true, false)
}

func writeTool(
	name string,
	description string,
	schema any,
	destructive bool,
) *mcp.Tool {
	return tool(name, description, schema, false, destructive)
}

func tool(
	name string,
	description string,
	schema any,
	readOnly bool,
	destructive bool,
) *mcp.Tool {
	openWorld := false
	return &mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    readOnly,
			DestructiveHint: &destructive,
			OpenWorldHint:   &openWorld,
		},
	}
}

//nolint:unparam // The zero output is required by the MCP SDK callback signature.
func failure[Output any](err error) (*mcp.CallToolResult, Output, error) {
	var zero Output
	return toolFailure(err), zero, nil
}

func toolFailure(err error) *mcp.CallToolResult {
	kind := "unexpected_error"
	message := "Autoboard tool failed unexpectedly."
	repair := "Read the current state and retry only if the outcome is known."
	var domainError *app.Error
	if errors.As(err, &domainError) {
		kind = string(domainError.Kind)
		message = domainError.Message
		repair = repairHint(domainError.Kind)
	}
	lines := []string{
		fmt.Sprintf("Autoboard %s: %s", kind, message),
		"Repair: " + repair,
	}
	if domainError != nil && len(domainError.Fields) > 0 {
		fields, marshalErr := json.Marshal(domainError.Fields)
		if marshalErr != nil {
			lines = append(lines, "Fields: unavailable")
		} else {
			lines = append(lines, "Fields: "+string(fields))
		}
	}
	if domainError != nil {
		var current any
		if domainError.CurrentProject != nil {
			current = domainError.CurrentProject
		} else if domainError.CurrentTicket != nil {
			current = domainError.CurrentTicket
		}
		if current != nil {
			encoded, marshalErr := json.Marshal(current)
			if marshalErr != nil {
				lines = append(lines, "Current entity: unavailable")
			} else {
				lines = append(lines, "Current entity: "+string(encoded))
			}
		}
	} else {
		lines = append(lines, "Correlation ID: "+uuid.NewString())
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: strings.Join(lines, "\n")},
		},
	}
}

func validationFailure(err error) *mcp.CallToolResult {
	return toolFailure(&app.Error{
		Kind:    app.ErrorValidationFailed,
		Message: "invalid tool arguments",
		Fields: map[string][]string{
			"arguments": {err.Error()},
		},
	})
}

func repairHint(kind app.ErrorKind) string {
	switch kind {
	case app.ErrorRevisionConflict:
		return "Read the latest entity, use its current revision, then retry the intended change."
	case app.ErrorValidationFailed:
		return "Correct the listed fields and retry with valid values."
	case app.ErrorNotFound:
		return "Read the project or ticket first and use its current ID or visible identifier."
	case app.ErrorInvalidTransition:
		return "Read the ticket's current state and choose a valid next status."
	case app.ErrorBlockedByDependency:
		return "Resolve or cancel the blocking tickets before changing this ticket."
	case app.ErrorDependencyCycle:
		return "Choose a dependency direction that does not create a cycle."
	case app.ErrorAttachmentFailed:
		return "Check that the source path is absolute, readable, and within the attachment size limit."
	case app.ErrorUnauthorized:
		return "Confirm this operation is allowed, then retry with an authorized request."
	default:
		return "Read the current state before deciding how to proceed."
	}
}

func priorityPointer(value *string) *app.Priority {
	if value == nil {
		return nil
	}
	converted := app.Priority(*value)
	return &converted
}

func assigneePointer(value *string) *app.Assignee {
	if value == nil {
		return nil
	}
	converted := app.Assignee(*value)
	return &converted
}

func presentAttachment(attachment app.Attachment) attachmentOutput {
	return attachmentOutput{
		ID:               attachment.ID,
		TicketID:         attachment.TicketID,
		ProjectID:        attachment.ProjectID,
		OriginalFilename: attachment.OriginalFilename,
		MediaType:        attachment.MediaType,
		ByteSize:         attachment.ByteSize,
		SHA256:           attachment.SHA256,
		Actor:            attachment.Actor,
		InsertedAt:       attachment.InsertedAt.Format(time.RFC3339Nano),
	}
}

func stringValue(value *string) string {
	if value == nil {
		return "unavailable"
	}
	return *value
}
