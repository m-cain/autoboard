package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/m-cain/autoboard/internal/app"
)

func Run(ctx context.Context, config Config, assets fs.FS) error {
	listener, err := (&net.ListenConfig{}).Listen(
		ctx,
		"tcp",
		config.Address,
	)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.Address, err)
	}
	return Serve(ctx, listener, config, assets)
}

func Serve(
	ctx context.Context,
	listener net.Listener,
	config Config,
	assets fs.FS,
) error {
	service, err := openService(ctx, config)
	if err != nil {
		_ = listener.Close()
		return err
	}
	defer func() {
		_ = service.Close()
	}()

	server := &http.Server{
		Handler:           NewHandler(service, assets, config),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			10*time.Second,
		)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		err := <-serveResult
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func openService(ctx context.Context, config Config) (*app.Service, error) {
	if err := os.MkdirAll(config.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Autoboard data directory: %w", err)
	}
	if err := os.Chmod(config.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("protect Autoboard data directory: %w", err)
	}
	databaseDir := filepath.Dir(config.DatabasePath)
	if err := os.MkdirAll(databaseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Autoboard database directory: %w", err)
	}
	if err := os.Chmod(databaseDir, 0o700); err != nil {
		return nil, fmt.Errorf("protect Autoboard database directory: %w", err)
	}
	service, err := app.Open(ctx, app.Config{
		DatabasePath:       config.DatabasePath,
		DataDir:            config.DataDir,
		MaxAttachmentBytes: config.MaxAttachmentBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("open Autoboard service: %w", err)
	}
	if err := os.Chmod(config.DatabasePath, 0o600); err != nil {
		_ = service.Close()
		return nil, fmt.Errorf("protect Autoboard database: %w", err)
	}
	return service, nil
}
