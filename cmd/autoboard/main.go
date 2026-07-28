package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
		SkillPath:  filepath.Join(home, ".agents", "skills", "autoboard"),
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
		err = uninstallManagedRuntime(ctx, manager, codexManager, codexManager)
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

type skillRemover interface {
	RemoveSkill() error
}

func uninstallManagedRuntime(
	ctx context.Context,
	runtimeManager runtimeUninstaller,
	codexManager codexRemover,
	skillManager skillRemover,
) error {
	runtimeErr := runtimeManager.Uninstall(ctx)
	codexErr := codexManager.Remove(ctx)
	skillErr := skillManager.RemoveSkill()
	if runtimeErr != nil {
		runtimeErr = fmt.Errorf("remove managed runtime: %w", runtimeErr)
	}
	if codexErr != nil {
		codexErr = fmt.Errorf("remove Codex registration: %w", codexErr)
	}
	if skillErr != nil {
		skillErr = fmt.Errorf("remove Codex skill: %w", skillErr)
	}
	return errors.Join(runtimeErr, codexErr, skillErr)
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
	skillManager := codexManager.SkillManager(
		filepath.Join(checkout, ".agents", "skills", "autoboard"),
	)
	if err := skillManager.Validate(); err != nil {
		return err
	}
	configSnapshot, err := snapshotFile(codexManager.ConfigPath)
	if err != nil {
		return err
	}
	skillSnapshot, err := snapshotSkill(codexManager.SkillPath)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(skillSnapshot.backup) }()
	record, err := installation.NewRecord(
		ctx,
		checkout,
		sourceExecutable,
		version,
	)
	if err != nil {
		return err
	}
	integrationAttempted := false
	err = manager.InstallVerified(
		ctx,
		sourceExecutable,
		func(ctx context.Context) error {
			if _, err := verifyEndpoint(ctx); err != nil {
				return err
			}
			integrationAttempted = true
			_, err := codexManager.Ensure(ctx)
			if err != nil {
				return err
			}
			_, err = skillManager.Ensure()
			if err != nil {
				return err
			}
			if err := installation.WriteRecord(paths.InstallRecord, record); err != nil {
				return err
			}
			return nil
		},
	)
	if err != nil && integrationAttempted {
		err = errors.Join(
			err,
			restoreFile(codexManager.ConfigPath, configSnapshot),
			restoreSkill(codexManager.SkillPath, skillSnapshot),
		)
	}
	return err
}

type fileSnapshot struct {
	exists  bool
	mode    os.FileMode
	content []byte
}

type skillSnapshot struct {
	exists bool
	backup string
}

var swapSkillDirectories = atomicSwapDirectories

var restoreFileRename = os.Rename

func snapshotFile(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{}, nil
	}
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("snapshot %s: %w", path, err)
	}
	if info.IsDir() {
		return fileSnapshot{}, fmt.Errorf("snapshot %s: path is a directory", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("snapshot %s: %w", path, err)
	}
	return fileSnapshot{exists: true, mode: info.Mode(), content: content}, nil
}

func restoreFile(path string, snapshot fileSnapshot) error {
	if !snapshot.exists {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restore absent %s: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("restore parent for %s: %w", path, err)
	}
	if err := writeRestoredFileAtomic(path, snapshot.content, snapshot.mode); err != nil {
		return fmt.Errorf("restore %s: %w", path, err)
	}
	return nil
}

func writeRestoredFileAtomic(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".autoboard-config-rollback-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return restoreFileRename(temporaryPath, path)
}

func snapshotSkill(directory string) (skillSnapshot, error) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return skillSnapshot{}, nil
	}
	if err != nil {
		return skillSnapshot{}, fmt.Errorf("snapshot installed skill: %w", err)
	}
	if !info.IsDir() {
		return skillSnapshot{}, errors.New("snapshot installed skill: path is not a directory")
	}
	backup, err := os.MkdirTemp(filepath.Dir(directory), ".autoboard-skill-rollback-")
	if err != nil {
		return skillSnapshot{}, fmt.Errorf("create installed skill snapshot: %w", err)
	}
	if err := copyDirectory(directory, backup); err != nil {
		_ = os.RemoveAll(backup)
		return skillSnapshot{}, err
	}
	return skillSnapshot{exists: true, backup: backup}, nil
}

func restoreSkill(directory string, snapshot skillSnapshot) error {
	if !snapshot.exists {
		if err := os.RemoveAll(directory); err != nil {
			return fmt.Errorf("restore installed skill: %w", err)
		}
		return nil
	}
	if err := swapSkillDirectories(directory, snapshot.backup); err != nil {
		return fmt.Errorf("restore installed skill: %w", err)
	}
	return nil
}

func copyDirectory(source string, destination string) error {
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return fmt.Errorf("open installed skill source: %w", err)
	}
	defer func() { _ = sourceRoot.Close() }()
	destinationRoot, err := os.OpenRoot(destination)
	if err != nil {
		return fmt.Errorf("open installed skill snapshot: %w", err)
	}
	defer func() { _ = destinationRoot.Close() }()
	return fs.WalkDir(sourceRoot.FS(), ".", func(relativePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if relativePath == "." {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			linkTarget, err := sourceRoot.Readlink(relativePath)
			if err != nil {
				return fmt.Errorf("read installed skill symlink: %w", err)
			}
			if err := destinationRoot.Symlink(linkTarget, relativePath); err != nil {
				return fmt.Errorf("copy installed skill symlink: %w", err)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect installed skill entry: %w", err)
		}
		if entry.IsDir() {
			if err := destinationRoot.MkdirAll(relativePath, info.Mode().Perm()); err != nil {
				return fmt.Errorf("copy installed skill directory: %w", err)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("copy installed skill entry %s: unsupported file type", relativePath)
		}
		content, err := sourceRoot.ReadFile(relativePath)
		if err != nil {
			return fmt.Errorf("read installed skill file: %w", err)
		}
		if err := destinationRoot.WriteFile(relativePath, content, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy installed skill file: %w", err)
		}
		return nil
	})
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
		skillStatus, skillErr := codexManager.SkillManager(
			filepath.Join(record.Checkout, ".agents", "skills", "autoboard"),
		).Status()
		switch {
		case skillErr != nil:
			fmt.Fprintf(output, "codex_skill: unavailable (%v)\n", skillErr)
			failures = append(failures, skillErr)
		case skillStatus != installation.SkillCurrent:
			fmt.Fprintf(output, "codex_skill: %s (%s)\n", skillStatus, codexManager.SkillPath)
			failures = append(failures, fmt.Errorf("autoboard Codex skill is %s", skillStatus))
		default:
			fmt.Fprintf(output, "codex_skill: current (%s)\n", codexManager.SkillPath)
		}
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
