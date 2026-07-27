package daemon_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/m-cain/autoboard/internal/daemon"
)

func TestRunStopsCleanlyAndCreatesPrivateState(t *testing.T) {
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Serve(
			ctx,
			listener,
			daemon.Config{
				Address:      listener.Addr().String(),
				DatabasePath: filepath.Join(root, "state", "autoboard.db"),
				DataDir:      filepath.Join(root, "state"),
			},
			fstest.MapFS{
				"index.html": &fstest.MapFile{Data: []byte("index")},
			},
		)
	}()
	healthURL := "http://" + listener.Addr().String() + "/health"
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, requestErr := http.Get(healthURL)
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not become healthy: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop after cancellation")
	}
}

func TestRunListensAndStopsWithItsContext(t *testing.T) {
	root := t.TempDir()
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve daemon address: %v", err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatalf("release daemon address: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(
			ctx,
			daemon.Config{
				Address:      address,
				DatabasePath: filepath.Join(root, "autoboard.db"),
				DataDir:      root,
			},
			fstest.MapFS{
				"index.html": &fstest.MapFile{Data: []byte("index")},
			},
		)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, requestErr := http.Get("http://" + address + "/health")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("daemon did not become healthy: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop after cancellation")
	}
}

func TestRunReportsListenerAndStatePreparationFailures(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy address: %v", err)
	}
	defer occupied.Close()
	root := t.TempDir()
	err = daemon.Run(
		context.Background(),
		daemon.Config{
			Address:      occupied.Addr().String(),
			DatabasePath: filepath.Join(root, "autoboard.db"),
			DataDir:      root,
		},
		fstest.MapFS{},
	)
	if err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("occupied address error = %v", err)
	}

	dataFile := filepath.Join(t.TempDir(), "data-file")
	if err := os.WriteFile(dataFile, []byte("file"), 0o600); err != nil {
		t.Fatalf("write data blocker: %v", err)
	}
	databaseFile := filepath.Join(t.TempDir(), "database-file")
	if err := os.WriteFile(databaseFile, []byte("file"), 0o600); err != nil {
		t.Fatalf("write database blocker: %v", err)
	}
	databaseDirectory := filepath.Join(t.TempDir(), "database")
	if err := os.Mkdir(databaseDirectory, 0o700); err != nil {
		t.Fatalf("create invalid database path: %v", err)
	}
	for _, test := range []struct {
		name         string
		dataDir      string
		databasePath string
		want         string
	}{
		{
			name:         "data directory",
			dataDir:      filepath.Join(dataFile, "nested"),
			databasePath: filepath.Join(root, "unused.db"),
			want:         "create Autoboard data directory",
		},
		{
			name:         "database directory",
			dataDir:      filepath.Join(t.TempDir(), "data"),
			databasePath: filepath.Join(databaseFile, "autoboard.db"),
			want:         "create Autoboard database directory",
		},
		{
			name:         "database",
			dataDir:      filepath.Join(t.TempDir(), "data"),
			databasePath: databaseDirectory,
			want:         "open Autoboard service",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runErr := daemon.Run(
				context.Background(),
				daemon.Config{
					Address:      "127.0.0.1:0",
					DatabasePath: test.databasePath,
					DataDir:      test.dataDir,
				},
				fstest.MapFS{},
			)
			if runErr == nil || !strings.Contains(runErr.Error(), test.want) {
				t.Fatalf("run error = %v, want %q", runErr, test.want)
			}
		})
	}
}
