package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/m-cain/autoboard/internal/app"
)

var (
	projectKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{1,7}$`)
	ticketRefPattern  = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9]{1,7}-[1-9][0-9]*$`,
	)
)

type Config struct {
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
}

type handler struct {
	service           *app.Service
	mux               *http.ServeMux
	pollInterval      time.Duration
	heartbeatInterval time.Duration
}

func New(service *app.Service, config Config) http.Handler {
	if config.PollInterval <= 0 {
		config.PollInterval = 250 * time.Millisecond
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = 15 * time.Second
	}
	h := &handler{
		service:           service,
		mux:               http.NewServeMux(),
		pollInterval:      config.PollInterval,
		heartbeatInterval: config.HeartbeatInterval,
	}
	h.routes()
	return h
}

func (h *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.NotFound(response, request)
		return
	}
	h.mux.ServeHTTP(response, request)
}

func (h *handler) routes() {
	h.mux.HandleFunc("GET /api/v1/projects", h.projects)
	h.mux.HandleFunc("GET /api/v1/triage", h.triage)
	h.mux.HandleFunc(
		"GET /api/v1/projects/{key}/board",
		h.projectBoard,
	)
	h.mux.HandleFunc(
		"GET /api/v1/projects/{key}/canceled",
		h.canceled,
	)
	h.mux.HandleFunc("GET /api/v1/tickets/{identifier}", h.ticket)
	h.mux.HandleFunc("GET /api/v1/attachments/{id}", h.attachment)
	h.mux.HandleFunc("GET /api/v1/events", h.events)
	h.mux.HandleFunc("GET /health", h.health)
	h.mux.HandleFunc("GET /api", h.apiNotFound)
	h.mux.HandleFunc("GET /api/", h.apiNotFound)
}

func (h *handler) projects(response http.ResponseWriter, request *http.Request) {
	projects, err := h.service.ListProjects(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, projects)
}

func (h *handler) triage(response http.ResponseWriter, request *http.Request) {
	tickets, err := h.service.ListTriageTickets(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"tickets": tickets})
}

func (h *handler) projectBoard(
	response http.ResponseWriter,
	request *http.Request,
) {
	key := request.PathValue("key")
	if !projectKeyPattern.MatchString(key) {
		writeError(response, requestValidation(
			"project_key",
			"must be a valid project key",
		))
		return
	}
	board, err := h.service.GetProjectBoard(request.Context(), key)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, board)
}

func (h *handler) canceled(response http.ResponseWriter, request *http.Request) {
	key := request.PathValue("key")
	if !projectKeyPattern.MatchString(key) {
		writeError(response, requestValidation(
			"project_key",
			"must be a valid project key",
		))
		return
	}
	tickets, err := h.service.ListCanceledTickets(request.Context(), key)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"tickets": tickets})
}

func (h *handler) ticket(response http.ResponseWriter, request *http.Request) {
	identifier := request.PathValue("identifier")
	if !ticketRefPattern.MatchString(identifier) {
		writeError(response, requestValidation(
			"identifier",
			"must be a valid ticket identifier",
		))
		return
	}
	detail, err := h.service.GetTicketDetail(request.Context(), identifier)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (h *handler) attachment(response http.ResponseWriter, request *http.Request) {
	id, err := uuid.Parse(request.PathValue("id"))
	if err != nil {
		writeError(response, requestValidation("id", "must be a valid UUID"))
		return
	}
	attachment, err := h.service.GetAttachment(request.Context(), id.String())
	if err != nil {
		writeError(response, err)
		return
	}
	file, err := os.Open(attachment.ManagedPath)
	if err != nil {
		writeError(response, appError(
			app.ErrorNotFound,
			"attachment not found",
		))
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(response, appError(
			app.ErrorNotFound,
			"attachment not found",
		))
		return
	}
	response.Header().Set("Content-Type", attachment.MediaType)
	response.Header().Set(
		"Content-Disposition",
		attachmentDisposition(attachment.OriginalFilename),
	)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "no-store")
	http.ServeContent(
		response,
		request,
		attachment.OriginalFilename,
		attachment.InsertedAt,
		file,
	)
}

func (h *handler) health(response http.ResponseWriter, request *http.Request) {
	report, err := h.service.HealthReport(request.Context())
	if err != nil {
		writeJSON(
			response,
			http.StatusServiceUnavailable,
			map[string]string{"status": "unavailable"},
		)
		return
	}
	writeJSON(response, http.StatusOK, report)
}

func (h *handler) apiNotFound(
	response http.ResponseWriter,
	_ *http.Request,
) {
	writeError(response, appError(app.ErrorNotFound, "route not found"))
}

func (h *handler) events(response http.ResponseWriter, request *http.Request) {
	cursor, err := eventCursor(request)
	if err != nil {
		writeError(response, err)
		return
	}
	highWater, err := h.service.HighWaterActivityID(request.Context())
	if err != nil {
		http.Error(
			response,
			"event stream unavailable",
			http.StatusServiceUnavailable,
		)
		return
	}
	if cursor > highWater {
		writeError(response, requestValidation(
			"last_event_id",
			"Last-Event-ID is newer than the activity log",
		))
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		http.Error(
			response,
			"event stream unavailable",
			http.StatusServiceUnavailable,
		)
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	response.WriteHeader(http.StatusOK)
	flusher.Flush()

	cursor, ok = h.drainEvents(
		request.Context(),
		response,
		flusher,
		cursor,
		highWater,
	)
	if !ok {
		return
	}
	poll := time.NewTicker(h.pollInterval)
	defer poll.Stop()
	heartbeat := time.NewTicker(h.heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-poll.C:
			highWater, err := h.service.HighWaterActivityID(request.Context())
			if err != nil {
				return
			}
			if cursor < highWater {
				cursor, ok = h.drainEvents(
					request.Context(),
					response,
					flusher,
					cursor,
					highWater,
				)
				if !ok {
					return
				}
			}
		case <-heartbeat.C:
			if _, err := io.WriteString(response, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *handler) drainEvents(
	ctx context.Context,
	response http.ResponseWriter,
	flusher http.Flusher,
	cursor int64,
	through int64,
) (int64, bool) {
	for cursor < through {
		events, err := h.service.ListActivityPage(ctx, cursor, through, 200)
		if err != nil {
			return cursor, false
		}
		if len(events) == 0 {
			return cursor, true
		}
		for _, event := range events {
			data, err := json.Marshal(event)
			if err != nil {
				return cursor, false
			}
			if _, err := fmt.Fprintf(
				response,
				"id: %d\nevent: activity\ndata: %s\n\n",
				event.ID,
				data,
			); err != nil {
				return cursor, false
			}
			cursor = event.ID
		}
		flusher.Flush()
	}
	return cursor, true
}

func eventCursor(request *http.Request) (int64, error) {
	values := request.Header.Values("Last-Event-ID")
	if len(values) == 0 {
		values = request.URL.Query()["last_event_id"]
	}
	if len(values) == 0 {
		return 0, nil
	}
	if len(values) != 1 {
		return 0, requestValidation(
			"last_event_id",
			"Last-Event-ID must be one non-negative integer",
		)
	}
	value, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || value < 0 {
		return 0, requestValidation(
			"last_event_id",
			"Last-Event-ID must be one non-negative integer",
		)
	}
	return value, nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		slog.Error("encode HTTP response", "error", err)
	}
}

func writeError(response http.ResponseWriter, err error) {
	var domainError *app.Error
	if !errors.As(err, &domainError) {
		correlationID := response.Header().Get("X-Request-ID")
		if correlationID == "" {
			correlationID = uuid.NewString()
			response.Header().Set("X-Request-ID", correlationID)
		}
		writeJSON(
			response,
			http.StatusInternalServerError,
			map[string]any{
				"error": map[string]any{
					"kind":           "internal_error",
					"message":        "internal error",
					"fields":         map[string][]string{},
					"current":        nil,
					"correlation_id": correlationID,
				},
			},
		)
		return
	}
	status := http.StatusInternalServerError
	switch domainError.Kind {
	case app.ErrorValidationFailed:
		status = http.StatusBadRequest
	case app.ErrorNotFound:
		status = http.StatusNotFound
	case app.ErrorUnauthorized:
		status = http.StatusForbidden
	case app.ErrorAttachmentFailed:
		status = http.StatusUnprocessableEntity
	case app.ErrorRevisionConflict,
		app.ErrorInvalidTransition,
		app.ErrorBlockedByDependency,
		app.ErrorDependencyCycle:
		status = http.StatusConflict
	}
	var current any
	if domainError.CurrentProject != nil {
		current = domainError.CurrentProject
	} else if domainError.CurrentTicket != nil {
		current = domainError.CurrentTicket
	}
	fields := domainError.Fields
	if fields == nil {
		fields = map[string][]string{}
	}
	writeJSON(response, status, map[string]any{
		"error": map[string]any{
			"kind":    domainError.Kind,
			"message": domainError.Message,
			"fields":  fields,
			"current": current,
		},
	})
}

func requestValidation(field string, message string) *app.Error {
	return &app.Error{
		Kind:    app.ErrorValidationFailed,
		Message: "request validation failed",
		Fields:  map[string][]string{field: {message}},
	}
}

func appError(kind app.ErrorKind, message string) *app.Error {
	return &app.Error{
		Kind:    kind,
		Message: message,
		Fields:  map[string][]string{},
	}
}

func attachmentDisposition(filename string) string {
	var safe strings.Builder
	for _, character := range filename {
		if character >= 0x20 &&
			character <= 0x7e &&
			character != '"' &&
			character != '\\' {
			safe.WriteRune(character)
		} else {
			safe.WriteByte('_')
		}
	}
	encoded := strings.ReplaceAll(url.PathEscape(filename), "+", "%20")
	return fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`,
		safe.String(),
		encoded,
	)
}
