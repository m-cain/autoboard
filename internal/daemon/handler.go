package daemon

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/m-cain/autoboard/internal/app"
	"github.com/m-cain/autoboard/internal/httpapi"
	"github.com/m-cain/autoboard/internal/mcpapi"
	"github.com/m-cain/autoboard/internal/webui"
)

const maxRequestBodyBytes = 1 << 20

func NewHandler(
	service *app.Service,
	assets fs.FS,
	config Config,
) http.Handler {
	api := httpapi.New(service, httpapi.Config{})
	mcpHandler := mcpapi.New(service)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)
	mux.Handle("/api", api)
	mux.Handle("/api/", api)
	mux.Handle("/health", api)
	mux.Handle("/", webui.New(assets))
	return requestSafety(localOnly(config, mux))
}

func localOnly(config Config, next http.Handler) http.Handler {
	allowedHosts := map[string]bool{config.Address: true}
	allowedOrigins := map[string]bool{"http://" + config.Address: true}
	if config.Development {
		for _, host := range []string{"127.0.0.1:5173", "localhost:5173"} {
			allowedHosts[host] = true
			allowedOrigins["http://"+host] = true
		}
	}
	return http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		peer, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			peer = request.RemoteAddr
		}
		address := net.ParseIP(strings.Trim(peer, "[]"))
		if address == nil || !address.IsLoopback() {
			http.Error(response, "forbidden", http.StatusForbidden)
			return
		}
		if !allowedHosts[request.Host] {
			http.Error(response, "forbidden", http.StatusForbidden)
			return
		}
		if origin := request.Header.Get("Origin"); origin != "" &&
			!allowedOrigins[origin] {
			http.Error(response, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func requestSafety(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		requestID := uuid.NewString()
		startedAt := time.Now()
		response.Header().Set("X-Request-ID", requestID)
		response.Header().Set("X-Content-Type-Options", "nosniff")
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error(
					"Autoboard HTTP panic",
					"request_id",
					requestID,
					"method",
					request.Method,
					"path",
					request.URL.Path,
					"error",
					recovered,
				)
				http.Error(
					response,
					"internal server error",
					http.StatusInternalServerError,
				)
			}
			slog.Debug(
				"Autoboard HTTP request",
				"request_id",
				requestID,
				"method",
				request.Method,
				"path",
				request.URL.Path,
				"remote_addr",
				request.RemoteAddr,
				"duration",
				time.Since(startedAt),
			)
		}()
		request.Body = http.MaxBytesReader(
			response,
			request.Body,
			maxRequestBodyBytes,
		)
		if request.URL.Path == "/api/v1/events" {
			next.ServeHTTP(response, request)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Minute)
		defer cancel()
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}
