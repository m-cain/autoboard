package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/m-cain/autoboard/internal/daemon"
	"github.com/m-cain/autoboard/internal/installation"
	"github.com/m-cain/autoboard/internal/launchagent"
	"github.com/m-cain/autoboard/internal/webui"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout io.Writer, stderr io.Writer) int {
	if len(arguments) == 0 {
		printUsage(stderr)
		return 2
	}
	switch arguments[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "serve":
		if len(arguments) != 1 {
			fmt.Fprintln(stderr, "serve does not accept arguments")
			return 2
		}
		config, err := daemon.ConfigFromEnvironment()
		if err != nil {
			fmt.Fprintf(stderr, "Autoboard: %v\n", err)
			return 1
		}
		ctx, stop := signal.NotifyContext(
			context.Background(),
			os.Interrupt,
			syscall.SIGTERM,
			syscall.SIGHUP,
		)
		defer stop()
		fmt.Fprintf(
			stderr,
			"Autoboard listening at http://%s (MCP: /mcp)\n",
			config.Address,
		)
		if err := daemon.Run(ctx, config, webui.Assets()); err != nil {
			fmt.Fprintf(stderr, "Autoboard: %v\n", err)
			return 1
		}
		return 0
	case "install", "update", "uninstall", "start", "stop", "restart", "status":
		if len(arguments) != 1 {
			fmt.Fprintf(stderr, "%s does not accept arguments\n", arguments[0])
			return 2
		}
		return runLifecycle(arguments[0], stdout, stderr)
	case "purge":
		if len(arguments) != 2 ||
			arguments[1] != "--confirm-delete-all-autoboard-data" {
			fmt.Fprintln(
				stderr,
				"purge requires --confirm-delete-all-autoboard-data",
			)
			return 2
		}
		return runLifecycle(arguments[0], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", arguments[0])
		printUsage(stderr)
		return 2
	}
}

func runLifecycle(command string, stdout io.Writer, stderr io.Writer) int {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(stderr, "Autoboard LaunchAgent commands require macOS")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "Autoboard: resolve home directory: %v\n", err)
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "Autoboard: resolve executable: %v\n", err)
		return 1
	}
	manager := launchagent.NewManager(
		launchagent.PathsForHome(home),
		os.Getuid(),
		launchagent.CommandLauncher{Stdout: stdout, Stderr: stderr},
	)
	paths := launchagent.PathsForHome(home)
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	codexManager := installation.CodexManager{
		ConfigPath: filepath.Join(codexHome, "config.toml"),
	}
	ctx := context.Background()
	switch command {
	case "install":
		checkout, checkoutErr := os.Getwd()
		if checkoutErr != nil {
			err = fmt.Errorf("resolve checkout: %w", checkoutErr)
			break
		}
		err = installRuntime(
			ctx,
			manager,
			codexManager,
			paths,
			executable,
			checkout,
		)
	case "update":
		err = updateRuntime(
			ctx,
			manager,
			codexManager,
			paths,
			stdout,
			stderr,
		)
	case "uninstall":
		err = uninstallManagedRuntime(ctx, manager, codexManager)
	case "start":
		err = manager.Start(ctx)
		if err == nil {
			_, err = verifyEndpoint(ctx)
		}
	case "stop":
		err = manager.Stop(ctx)
	case "restart":
		err = manager.Restart(ctx)
		if err == nil {
			_, err = verifyEndpoint(ctx)
		}
	case "status":
		err = printStatus(
			ctx,
			manager,
			codexManager,
			paths,
			stdout,
		)
	case "purge":
		err = purgeManagedData(ctx, manager, paths)
	}
	if err != nil {
		fmt.Fprintf(stderr, "Autoboard: %v\n", err)
		return 1
	}
	if command != "status" {
		fmt.Fprintf(stdout, "Autoboard %s complete.\n", command)
	}
	return 0
}

type runtimeUninstaller interface {
	Uninstall(context.Context) error
}

type codexRemover interface {
	Remove(context.Context) error
}

func uninstallManagedRuntime(
	ctx context.Context,
	runtimeManager runtimeUninstaller,
	codexManager codexRemover,
) error {
	runtimeErr := runtimeManager.Uninstall(ctx)
	codexErr := codexManager.Remove(ctx)
	if runtimeErr != nil {
		runtimeErr = fmt.Errorf("remove managed runtime: %w", runtimeErr)
	}
	if codexErr != nil {
		codexErr = fmt.Errorf("remove Codex registration: %w", codexErr)
	}
	return errors.Join(runtimeErr, codexErr)
}

type launchAgentState interface {
	Loaded(context.Context) (bool, error)
}

func purgeManagedData(
	ctx context.Context,
	manager launchAgentState,
	paths launchagent.Paths,
) error {
	loaded, err := manager.Loaded(ctx)
	if err != nil {
		return fmt.Errorf(
			"confirm Autoboard LaunchAgent is unloaded before purge: %w",
			err,
		)
	}
	if loaded {
		return errors.New("stop and uninstall Autoboard before purging data")
	}
	for _, ownedRuntimePath := range []string{
		paths.Plist,
		paths.Executable,
	} {
		if _, statErr := os.Stat(ownedRuntimePath); statErr == nil {
			return fmt.Errorf(
				"uninstall Autoboard before purging data; found %s",
				ownedRuntimePath,
			)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf(
				"inspect Autoboard runtime before purge: %w",
				statErr,
			)
		}
	}
	return os.RemoveAll(paths.DataDir)
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `Usage: autoboard <command>

Commands:
  serve       Run the loopback HTTP, MCP, and browser daemon
  install     Install the macOS service and register it with Codex
  update      Rebuild from the recorded checkout and update the service
  uninstall   Remove the LaunchAgent and installed binary; preserve board data
  start       Load the installed LaunchAgent
  stop        Unload the installed LaunchAgent
  restart     Reload the installed LaunchAgent
  status      Report service, health, version, paths, and Codex registration
  purge       Delete preserved data; requires the explicit confirmation flag
  version     Print the daemon version
  help        Show this help`)
}

func installRuntime(
	ctx context.Context,
	manager *launchagent.Manager,
	codexManager installation.CodexManager,
	paths launchagent.Paths,
	sourceExecutable string,
	checkout string,
) error {
	if _, err := codexManager.Validate(ctx); err != nil {
		return err
	}
	record, err := installation.NewRecord(
		ctx,
		checkout,
		sourceExecutable,
		version,
	)
	if err != nil {
		return err
	}
	addedCodexRegistration := false
	err = manager.InstallVerified(
		ctx,
		sourceExecutable,
		func(ctx context.Context) error {
			if _, err := verifyEndpoint(ctx); err != nil {
				return err
			}
			added, err := codexManager.Ensure(ctx)
			if err != nil {
				return err
			}
			addedCodexRegistration = added
			if err := installation.WriteRecord(paths.InstallRecord, record); err != nil {
				if added {
					_ = codexManager.Remove(ctx)
					addedCodexRegistration = false
				}
				return err
			}
			return nil
		},
	)
	if err != nil && addedCodexRegistration {
		_ = codexManager.Remove(ctx)
	}
	return err
}

func updateRuntime(
	ctx context.Context,
	manager *launchagent.Manager,
	codexManager installation.CodexManager,
	paths launchagent.Paths,
	stdout io.Writer,
	stderr io.Writer,
) error {
	record, err := installation.ReadRecord(paths.InstallRecord)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "just", "build")
	command.Dir = record.Checkout
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build Autoboard update: %w", err)
	}
	return installRuntime(
		ctx,
		manager,
		codexManager,
		paths,
		filepath.Join(record.Checkout, "dist", "autoboard"),
		record.Checkout,
	)
}

type endpointHealth struct {
	Status                string `json:"status"`
	SchemaVersion         int64  `json:"schema_version"`
	ActivityHighWater     int64  `json:"activity_high_water"`
	AttachmentWritable    bool   `json:"attachment_writable"`
	OrphanAttachmentFiles int    `json:"orphan_attachment_files"`
}

func verifyEndpoint(ctx context.Context) (endpointHealth, error) {
	healthURL := strings.TrimSuffix(installation.MCPURL, "/mcp") + "/health"
	deadline := time.Now().Add(15 * time.Second)
	client := &http.Client{Timeout: time.Second}
	var lastError error
	var health endpointHealth
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return endpointHealth{}, err
		}
		response, err := client.Do(request)
		if err == nil {
			decodeErr := json.NewDecoder(response.Body).Decode(&health)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK &&
				decodeErr == nil &&
				health.Status == "ok" &&
				health.AttachmentWritable {
				lastError = nil
				break
			}
			lastError = errors.Join(
				fmt.Errorf("health returned HTTP %d", response.StatusCode),
				decodeErr,
			)
		} else {
			lastError = err
		}
		select {
		case <-ctx.Done():
			return endpointHealth{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if lastError != nil {
		return endpointHealth{}, fmt.Errorf(
			"autoboard did not become healthy: %w",
			lastError,
		)
	}
	mcpClient := mcp.NewClient(
		&mcp.Implementation{Name: "autoboard-installer", Version: version},
		nil,
	)
	session, err := mcpClient.Connect(
		ctx,
		&mcp.StreamableClientTransport{
			Endpoint:             installation.MCPURL,
			DisableStandaloneSSE: true,
		},
		nil,
	)
	if err != nil {
		return endpointHealth{}, fmt.Errorf(
			"initialize installed MCP endpoint: %w",
			err,
		)
	}
	defer func() {
		_ = session.Close()
	}()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return endpointHealth{}, fmt.Errorf(
			"list installed MCP tools: %w",
			err,
		)
	}
	if len(tools.Tools) != 17 {
		return endpointHealth{}, fmt.Errorf(
			"installed MCP endpoint exposed %d tools, want 17",
			len(tools.Tools),
		)
	}
	return health, nil
}

func printStatus(
	ctx context.Context,
	manager *launchagent.Manager,
	codexManager installation.CodexManager,
	paths launchagent.Paths,
	output io.Writer,
) error {
	var failures []error
	launchAgentLoaded := true
	if err := manager.Status(ctx); err != nil {
		launchAgentLoaded = false
		fmt.Fprintf(output, "launch_agent: unavailable (%v)\n", err)
		failures = append(failures, err)
	} else {
		fmt.Fprintln(output, "launch_agent: loaded")
	}
	record, err := installation.ReadRecord(paths.InstallRecord)
	if err != nil {
		fmt.Fprintf(output, "installation_record: unavailable (%v)\n", err)
		failures = append(failures, err)
	} else {
		fmt.Fprintf(output, "checkout: %s\n", record.Checkout)
		fmt.Fprintf(output, "checkout_revision: %s\n", record.CheckoutRevision)
		currentRevision, revisionErr := installation.CheckoutRevision(
			ctx,
			record.Checkout,
		)
		if revisionErr != nil {
			fmt.Fprintf(
				output,
				"current_checkout_revision: unavailable (%v)\n",
				revisionErr,
			)
			failures = append(failures, revisionErr)
		} else {
			fmt.Fprintf(
				output,
				"current_checkout_revision: %s\n",
				currentRevision,
			)
		}
		fmt.Fprintf(output, "recorded_binary_sha256: %s\n", record.BinarySHA256)
		fmt.Fprintf(output, "version: %s\n", record.Version)
	}
	fmt.Fprintf(output, "data_directory: %s\n", paths.DataDir)
	fmt.Fprintf(output, "binary: %s\n", paths.Executable)
	if fingerprint, fingerprintErr := installation.FileSHA256(
		paths.Executable,
	); fingerprintErr != nil {
		fmt.Fprintf(output, "installed_binary_sha256: unavailable (%v)\n", fingerprintErr)
		failures = append(failures, fingerprintErr)
	} else {
		fmt.Fprintf(output, "installed_binary_sha256: %s\n", fingerprint)
	}
	if !launchAgentLoaded {
		fmt.Fprintln(output, "health: unavailable (LaunchAgent is not loaded)")
	} else {
		health, healthErr := verifyEndpoint(ctx)
		if healthErr != nil {
			fmt.Fprintf(output, "health: unavailable (%v)\n", healthErr)
			failures = append(failures, healthErr)
		} else {
			fmt.Fprintf(
				output,
				"health: ok (schema=%d activity_high_water=%d orphan_attachments=%d MCP=17)\n",
				health.SchemaVersion,
				health.ActivityHighWater,
				health.OrphanAttachmentFiles,
			)
		}
	}
	codexStatus, codexErr := codexManager.Status(ctx)
	switch {
	case codexErr != nil:
		fmt.Fprintf(output, "codex: unavailable (%v)\n", codexErr)
		failures = append(failures, codexErr)
	case codexStatus.Registered:
		fmt.Fprintf(output, "codex: %s\n", codexStatus.URL)
	default:
		fmt.Fprintln(output, "codex: not registered")
		failures = append(failures, errors.New("codex MCP registration is missing"))
	}
	return errors.Join(failures...)
}
