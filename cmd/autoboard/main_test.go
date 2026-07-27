package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/m-cain/autoboard/internal/daemon"
	"github.com/m-cain/autoboard/internal/installation"
	"github.com/m-cain/autoboard/internal/launchagent"
)

type fakeRuntimeManager struct {
	loaded       bool
	stateErr     error
	uninstallErr error
	uninstalled  bool
}

func (f *fakeRuntimeManager) Loaded(context.Context) (bool, error) {
	return f.loaded, f.stateErr
}

func (f *fakeRuntimeManager) Uninstall(context.Context) error {
	f.uninstalled = true
	return f.uninstallErr
}

type fakeCodexRemover struct {
	err     error
	removed bool
}

func (f *fakeCodexRemover) Remove(context.Context) error {
	f.removed = true
	return f.err
}

type fakeInstallLauncher struct {
	calls  [][]string
	loaded bool
}

func (f *fakeInstallLauncher) Run(
	_ context.Context,
	arguments ...string,
) error {
	f.calls = append(f.calls, slices.Clone(arguments))
	switch arguments[0] {
	case "bootstrap":
		f.loaded = true
	case "bootout":
		f.loaded = false
	}
	return nil
}

func (f *fakeInstallLauncher) Loaded(
	_ context.Context,
	arguments ...string,
) (bool, error) {
	f.calls = append(f.calls, slices.Clone(arguments))
	return f.loaded, nil
}

func (f *fakeInstallLauncher) Bootstrap(
	ctx context.Context,
	arguments ...string,
) error {
	return f.Run(ctx, arguments...)
}

type configAwareCodexRunner struct {
	configPath string
}

func (r configAwareCodexRunner) Output(
	_ context.Context,
	_ string,
	_ ...string,
) ([]byte, error) {
	content, err := os.ReadFile(r.configPath)
	if errors.Is(err, os.ErrNotExist) {
		return []byte("[]"), nil
	}
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(content, []byte("[mcp_servers.autoboard]")) {
		return []byte("[]"), nil
	}
	return fmt.Appendf(nil,
		`[{"name":"autoboard","transport":{"type":"streamable_http","url":%q}}]`,
		installation.MCPURL,
	), nil
}

func TestHelpDocumentsDaemonAndLifecycleCommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit code = %d, stderr=%s", code, stderr.String())
	}
	for _, command := range []string{
		"serve",
		"install",
		"update",
		"uninstall",
		"start",
		"stop",
		"restart",
		"status",
		"purge",
		"version",
	} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help missing %q:\n%s", command, stdout.String())
		}
	}
}

func TestRunValidatesCommandArgumentsAndReportsVersion(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		wantCode  int
		wantText  string
	}{
		{
			name:     "missing command",
			wantCode: 2,
			wantText: "Usage:",
		},
		{
			name:      "help flag",
			arguments: []string{"--help"},
			wantCode:  0,
			wantText:  "Commands:",
		},
		{
			name:      "version",
			arguments: []string{"--version"},
			wantCode:  0,
			wantText:  version,
		},
		{
			name:      "serve arguments",
			arguments: []string{"serve", "extra"},
			wantCode:  2,
			wantText:  "serve does not accept arguments",
		},
		{
			name:      "lifecycle arguments",
			arguments: []string{"status", "extra"},
			wantCode:  2,
			wantText:  "status does not accept arguments",
		},
		{
			name:      "purge confirmation",
			arguments: []string{"purge"},
			wantCode:  2,
			wantText:  "purge requires",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := run(test.arguments, &stdout, &stderr)
			if code != test.wantCode {
				t.Fatalf("exit code = %d, want %d", code, test.wantCode)
			}
			if output := stdout.String() + stderr.String(); !strings.Contains(
				output,
				test.wantText,
			) {
				t.Fatalf("output = %q, want %q", output, test.wantText)
			}
		})
	}
}

func TestUnknownCommandFailsWithoutStartingAnything(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestServeRejectsInvalidEnvironmentBeforeListening(t *testing.T) {
	t.Setenv("AUTOBOARD_HTTP_PORT", "not-a-port")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"serve"}, &stdout, &stderr); code != 1 {
		t.Fatalf("serve exit code = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "AUTOBOARD_HTTP_PORT") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestUninstallAttemptsRuntimeAndCodexCleanupIndependently(t *testing.T) {
	runtimeManager := &fakeRuntimeManager{}
	codexManager := &fakeCodexRemover{err: errors.New("codex unavailable")}

	err := uninstallManagedRuntime(
		context.Background(),
		runtimeManager,
		codexManager,
	)
	if err == nil {
		t.Fatal("uninstall succeeded, want Codex cleanup failure")
	}
	if !runtimeManager.uninstalled || !codexManager.removed {
		t.Fatalf(
			"uninstalled runtime=%v removed Codex=%v, want both true",
			runtimeManager.uninstalled,
			codexManager.removed,
		)
	}
}

func TestPurgeRequiresConfirmedUnloadedLaunchAgentState(t *testing.T) {
	for _, test := range []struct {
		name    string
		manager *fakeRuntimeManager
	}{
		{
			name:    "loaded",
			manager: &fakeRuntimeManager{loaded: true},
		},
		{
			name: "indeterminate",
			manager: &fakeRuntimeManager{
				stateErr: errors.New("launchctl unavailable"),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			paths := launchagent.PathsForHome(home)
			if err := os.MkdirAll(paths.DataDir, 0o700); err != nil {
				t.Fatalf("create data directory: %v", err)
			}
			marker := filepath.Join(paths.DataDir, "autoboard.db")
			if err := os.WriteFile(marker, []byte("state"), 0o600); err != nil {
				t.Fatalf("write marker: %v", err)
			}

			if err := purgeManagedData(
				context.Background(),
				test.manager,
				paths,
			); err == nil {
				t.Fatal("purge succeeded without confirmed unloaded state")
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("purge removed data after uncertain state: %v", err)
			}
		})
	}
}

func TestInstallStatusAndUninstallManagedRuntime(t *testing.T) {
	stopEndpoint := startEndpoint(t)
	defer stopEndpoint()

	home := t.TempDir()
	paths := launchagent.PathsForHome(home)
	sourceExecutable := filepath.Join(t.TempDir(), "autoboard")
	if err := os.WriteFile(sourceExecutable, []byte("test binary"), 0o700); err != nil {
		t.Fatalf("write source executable: %v", err)
	}
	checkout, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve checkout: %v", err)
	}
	launcher := &fakeInstallLauncher{}
	manager := launchagent.NewManager(paths, 501, launcher)
	configPath := filepath.Join(home, ".codex", "config.toml")
	codexManager := installation.CodexManager{
		Runner:     configAwareCodexRunner{configPath: configPath},
		ConfigPath: configPath,
	}
	ctx := context.Background()

	if err := installRuntime(
		ctx,
		manager,
		codexManager,
		paths,
		sourceExecutable,
		checkout,
	); err != nil {
		t.Fatalf("install managed runtime: %v", err)
	}
	var status bytes.Buffer
	if err := printStatus(
		ctx,
		manager,
		codexManager,
		paths,
		&status,
	); err != nil {
		t.Fatalf("print installed status: %v\n%s", err, status.String())
	}
	for _, fragment := range []string{
		"launch_agent: loaded",
		"checkout_revision:",
		"installed_binary_sha256:",
		"health: ok",
		"codex: " + installation.MCPURL,
	} {
		if !strings.Contains(status.String(), fragment) {
			t.Errorf("status missing %q:\n%s", fragment, status.String())
		}
	}

	if err := uninstallManagedRuntime(ctx, manager, codexManager); err != nil {
		t.Fatalf("uninstall managed runtime: %v", err)
	}
	if launcher.loaded {
		t.Fatal("LaunchAgent remains loaded after uninstall")
	}
	for _, path := range []string{
		paths.Executable,
		paths.Plist,
		paths.InstallRecord,
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("managed path %s remains after uninstall: %v", path, err)
		}
	}
}

func TestLifecycleCommandsHandleAbsentInstallationSafely(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	writeTestExecutable(
		t,
		filepath.Join(bin, "launchctl"),
		"#!/bin/sh\nif [ \"$1\" = \"print\" ]; then\n  printf '%s\\n' 'Could not find service' >&2\n  exit 3\nfi\nexit 0\n",
	)
	writeTestExecutable(
		t,
		filepath.Join(bin, "codex"),
		"#!/bin/sh\nprintf '[]\\n'\n",
	)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("PATH", bin)

	for _, test := range []struct {
		command  string
		wantCode int
	}{
		{command: "stop", wantCode: 0},
		{command: "update", wantCode: 1},
		{command: "status", wantCode: 1},
		{command: "purge", wantCode: 0},
		{command: "uninstall", wantCode: 0},
	} {
		t.Run(test.command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := runLifecycle(
				test.command,
				&stdout,
				&stderr,
			); code != test.wantCode {
				t.Fatalf(
					"%s exit code = %d, want %d; stdout=%s stderr=%s",
					test.command,
					code,
					test.wantCode,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestUpdateRuntimeBuildsRecordedCheckoutAndReinstalls(t *testing.T) {
	stopEndpoint := startEndpoint(t)
	defer stopEndpoint()

	checkout := initializeTestCheckout(t)
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "outer.git"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	bin := t.TempDir()
	writeTestExecutable(
		t,
		filepath.Join(bin, "just"),
		"#!/bin/sh\n/bin/mkdir -p dist\nprintf '%s\\n' 'updated binary' > dist/autoboard\n",
	)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	home := t.TempDir()
	paths := launchagent.PathsForHome(home)
	if err := os.MkdirAll(paths.DataDir, 0o700); err != nil {
		t.Fatalf("create managed data directory: %v", err)
	}
	if err := installation.WriteRecord(
		paths.InstallRecord,
		installation.Record{
			Checkout:         checkout,
			CheckoutRevision: "previous",
			BinarySHA256:     "previous",
			Version:          "previous",
			InstalledAt:      time.Now().UTC(),
		},
	); err != nil {
		t.Fatalf("write prior installation record: %v", err)
	}
	launcher := &fakeInstallLauncher{}
	manager := launchagent.NewManager(paths, 501, launcher)
	configPath := filepath.Join(home, ".codex", "config.toml")
	codexManager := installation.CodexManager{
		Runner:     configAwareCodexRunner{configPath: configPath},
		ConfigPath: configPath,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := updateRuntime(
		context.Background(),
		manager,
		codexManager,
		paths,
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("update runtime: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	installed, err := os.ReadFile(paths.Executable)
	if err != nil {
		t.Fatalf("read updated executable: %v", err)
	}
	if string(installed) != "updated binary\n" {
		t.Errorf("updated executable = %q", installed)
	}
	record, err := installation.ReadRecord(paths.InstallRecord)
	if err != nil {
		t.Fatalf("read updated installation record: %v", err)
	}
	if record.Checkout != checkout || record.BinarySHA256 == "previous" {
		t.Errorf("updated installation record = %#v", record)
	}
}

func TestVerifyEndpointHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifyEndpoint(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("verify canceled endpoint error = %v", err)
	}
}

func startEndpoint(t *testing.T) func() {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:4040")
	if err != nil {
		t.Fatalf("listen for managed endpoint: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	root := t.TempDir()
	go func() {
		done <- daemon.Serve(
			ctx,
			listener,
			daemon.Config{
				Address:      listener.Addr().String(),
				DatabasePath: filepath.Join(root, "autoboard.db"),
				DataDir:      root,
			},
			fstest.MapFS{
				"index.html": &fstest.MapFile{Data: []byte("Autoboard")},
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
			cancel()
			t.Fatalf("managed endpoint did not become healthy: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() {
		cancel()
		select {
		case serveErr := <-done:
			if serveErr != nil {
				t.Errorf("stop managed endpoint: %v", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("managed endpoint did not stop")
		}
	}
}

func writeTestExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func initializeTestCheckout(t *testing.T) string {
	t.Helper()
	checkout := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(checkout, "go.mod"),
		[]byte("module example.com/autoboard-update\n\ngo 1.25\n"),
		0o600,
	); err != nil {
		t.Fatalf("write test go.mod: %v", err)
	}
	for _, arguments := range [][]string{
		{"init"},
		{"add", "go.mod"},
		{
			"-c",
			"user.name=Autoboard Test",
			"-c",
			"user.email=autoboard@example.invalid",
			"commit",
			"-m",
			"initial",
		},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = checkout
		command.Env = withoutTestGitRepositoryEnvironment(os.Environ())
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, output)
		}
	}
	return checkout
}

func withoutTestGitRepositoryEnvironment(environment []string) []string {
	clean := make([]string, 0, len(environment))
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		switch name {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_CONFIG",
			"GIT_CONFIG_PARAMETERS",
			"GIT_CONFIG_COUNT",
			"GIT_OBJECT_DIRECTORY",
			"GIT_DIR",
			"GIT_WORK_TREE",
			"GIT_IMPLICIT_WORK_TREE",
			"GIT_GRAFT_FILE",
			"GIT_INDEX_FILE",
			"GIT_NO_REPLACE_OBJECTS",
			"GIT_REPLACE_REF_BASE",
			"GIT_PREFIX",
			"GIT_SHALLOW_FILE",
			"GIT_COMMON_DIR":
			continue
		default:
			clean = append(clean, variable)
		}
	}
	return clean
}
