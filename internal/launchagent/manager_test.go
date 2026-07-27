package launchagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeLauncher struct {
	calls    [][]string
	fail     map[string]error
	failOnce map[string]error
	loaded   bool
	stateErr error
}

func (f *fakeLauncher) Run(_ context.Context, arguments ...string) error {
	f.calls = append(f.calls, slices.Clone(arguments))
	key := strings.Join(arguments, " ")
	if err := f.failOnce[key]; err != nil {
		delete(f.failOnce, key)
		return err
	}
	if err := f.fail[key]; err != nil {
		return err
	}
	switch arguments[0] {
	case "print":
		if !f.loaded {
			return errors.New("not loaded")
		}
	case "bootstrap":
		if f.loaded {
			return errors.New("already loaded")
		}
		f.loaded = true
	case "bootout":
		if !f.loaded {
			return errors.New("not loaded")
		}
		f.loaded = false
	}
	return nil
}

func TestRestartRetriesTransientLaunchdBootstrapError(t *testing.T) {
	paths := PathsForHome(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	if err := os.WriteFile(paths.Plist, []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	key := "bootstrap gui/501 " + paths.Plist
	launcher := &fakeLauncher{
		fail:     map[string]error{},
		failOnce: map[string]error{key: errors.New("exit status 5")},
		loaded:   true,
	}
	manager := NewManager(paths, 501, launcher)
	if err := manager.Restart(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !launcher.loaded {
		t.Fatal("LaunchAgent is not loaded after retry")
	}
}

func (f *fakeLauncher) Loaded(
	_ context.Context,
	arguments ...string,
) (bool, error) {
	f.calls = append(f.calls, slices.Clone(arguments))
	if f.stateErr != nil {
		return false, f.stateErr
	}
	return f.loaded, nil
}

func (f *fakeLauncher) Bootstrap(
	ctx context.Context,
	arguments ...string,
) error {
	return f.Run(ctx, arguments...)
}

func TestInstallCopiesTheBinaryWritesAPrivateLaunchAgentAndBootstraps(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "autoboard")
	if err := os.WriteFile(source, []byte("go-binary"), 0o755); err != nil {
		t.Fatalf("write source executable: %v", err)
	}
	launcher := &fakeLauncher{fail: map[string]error{}}
	manager := NewManager(PathsForHome(home), 501, launcher)
	if err := manager.Install(context.Background(), source); err != nil {
		t.Fatalf("install: %v", err)
	}

	installed, err := os.ReadFile(manager.paths.Executable)
	if err != nil {
		t.Fatalf("read installed executable: %v", err)
	}
	info, err := os.Stat(manager.paths.Executable)
	if err != nil {
		t.Fatalf("stat installed executable: %v", err)
	}
	if string(installed) != "go-binary" || info.Mode().Perm() != 0o755 {
		t.Errorf("installed binary mode=%o body=%q", info.Mode().Perm(), installed)
	}
	plist, err := os.ReadFile(manager.paths.Plist)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	for _, fragment := range []string{
		"<string>" + Label + "</string>",
		"<string>" + manager.paths.Executable + "</string>",
		"<string>serve</string>",
		"<key>KeepAlive</key>",
		"<key>RunAtLoad</key>",
		"<integer>63</integer>",
		manager.paths.StandardOut,
		manager.paths.StandardError,
	} {
		if !strings.Contains(string(plist), fragment) {
			t.Errorf("plist missing %q:\n%s", fragment, plist)
		}
	}
	wantCalls := [][]string{
		{"print", "gui/501/" + Label},
		{"bootstrap", "gui/501", manager.paths.Plist},
	}
	if !slices.EqualFunc(
		launcher.calls,
		wantCalls,
		slices.Equal,
	) {
		t.Errorf("launchctl calls = %q, want %q", launcher.calls, wantCalls)
	}
}

func TestUninstallRemovesOnlyManagedRuntimeFiles(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	if err := os.MkdirAll(filepath.Dir(paths.Executable), 0o700); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	if err := os.WriteFile(paths.Executable, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	manager := NewManager(
		paths,
		501,
		&fakeLauncher{fail: map[string]error{}, loaded: true},
	)
	if err := os.WriteFile(paths.Plist, manager.plist(), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	database := filepath.Join(paths.DataDir, "autoboard.db")
	if err := os.WriteFile(database, []byte("state"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}
	if err := manager.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	for _, removed := range []string{paths.Executable, paths.Plist} {
		if _, err := os.Stat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists or stat failed: %v", removed, err)
		}
	}
	if state, err := os.ReadFile(database); err != nil || string(state) != "state" {
		t.Errorf("database was not preserved: body=%q err=%v", state, err)
	}
}

func TestLifecycleCommandsUseTheUserLaunchdDomain(t *testing.T) {
	paths := PathsForHome(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	if err := os.WriteFile(paths.Plist, []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	launcher := &fakeLauncher{fail: map[string]error{}}
	manager := NewManager(paths, 502, launcher)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("idempotent start: %v", err)
	}
	if err := manager.Restart(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatalf("idempotent stop: %v", err)
	}
	if err := manager.Status(ctx); err != nil {
		t.Logf("status correctly reports unloaded service: %v", err)
	}
	wantCalls := [][]string{
		{"print", "gui/502/" + Label},
		{"bootstrap", "gui/502", paths.Plist},
		{"print", "gui/502/" + Label},
		{"print", "gui/502/" + Label},
		{"bootout", "gui/502/" + Label},
		{"bootstrap", "gui/502", paths.Plist},
		{"print", "gui/502/" + Label},
		{"bootout", "gui/502/" + Label},
		{"print", "gui/502/" + Label},
		{"print", "gui/502/" + Label},
	}
	if !slices.EqualFunc(
		launcher.calls,
		wantCalls,
		slices.Equal,
	) {
		t.Errorf("launchctl calls = %q, want %q", launcher.calls, wantCalls)
	}
}

func TestLifecycleCommandsPropagateIndeterminateLaunchdState(t *testing.T) {
	paths := PathsForHome(t.TempDir())
	launcher := &fakeLauncher{
		fail:     map[string]error{},
		stateErr: errors.New("launchctl permission denied"),
	}
	manager := NewManager(paths, 502, launcher)

	loaded, err := manager.Loaded(context.Background())
	if err == nil || loaded {
		t.Fatalf("Loaded() = %v, %v; want false and an error", loaded, err)
	}
	if err := manager.Start(context.Background()); err == nil {
		t.Fatal("start succeeded with indeterminate launchd state")
	}
	if len(launcher.calls) != 2 {
		t.Fatalf("launchctl calls = %q, want two state probes", launcher.calls)
	}
}

func TestInstallRestoresPreviousRuntimeWhenBootstrapFails(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	if err := os.MkdirAll(filepath.Dir(paths.Executable), 0o700); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	source := filepath.Join(t.TempDir(), "autoboard")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	manager := NewManager(paths, 501, &fakeLauncher{fail: map[string]error{}})
	if err := os.WriteFile(paths.Executable, []byte("old"), 0o755); err != nil {
		t.Fatalf("write old executable: %v", err)
	}
	if err := os.WriteFile(paths.Plist, manager.plist(), 0o644); err != nil {
		t.Fatalf("write old plist: %v", err)
	}
	launcher := &fakeLauncher{
		fail: map[string]error{
			"bootstrap gui/501 " + paths.Plist: errors.New("new service failed"),
		},
		loaded: true,
	}
	manager = NewManager(paths, 501, launcher)
	if err := manager.Install(context.Background(), source); err == nil {
		t.Fatal("install succeeded, want bootstrap failure")
	}
	content, err := os.ReadFile(paths.Executable)
	if err != nil {
		t.Fatalf("read restored executable: %v", err)
	}
	if string(content) != "old" {
		t.Errorf("restored executable = %q, want old", content)
	}
}

func TestInstallRestoresPreviousRuntimeWhenVerificationFails(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	if err := os.MkdirAll(filepath.Dir(paths.Executable), 0o700); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	source := filepath.Join(t.TempDir(), "autoboard")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	launcher := &fakeLauncher{fail: map[string]error{}, loaded: true}
	manager := NewManager(paths, 501, launcher)
	if err := os.WriteFile(paths.Executable, []byte("old"), 0o755); err != nil {
		t.Fatalf("write old executable: %v", err)
	}
	if err := os.WriteFile(paths.Plist, manager.plist(), 0o644); err != nil {
		t.Fatalf("write old plist: %v", err)
	}
	err := manager.InstallVerified(
		context.Background(),
		source,
		func(context.Context) error { return errors.New("unhealthy") },
	)
	if err == nil {
		t.Fatal("install succeeded, want verification failure")
	}
	content, readErr := os.ReadFile(paths.Executable)
	if readErr != nil {
		t.Fatalf("read restored executable: %v", readErr)
	}
	if string(content) != "old" || !launcher.loaded {
		t.Errorf(
			"rollback executable=%q loaded=%v, want old and loaded",
			content,
			launcher.loaded,
		)
	}
}

func TestInstallRefusesConflictingLaunchAgent(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	if err := os.WriteFile(paths.Plist, []byte("other service"), 0o644); err != nil {
		t.Fatalf("write conflicting plist: %v", err)
	}
	source := filepath.Join(t.TempDir(), "autoboard")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	launcher := &fakeLauncher{fail: map[string]error{}}
	manager := NewManager(paths, 501, launcher)
	if err := manager.Install(context.Background(), source); err == nil {
		t.Fatal("install succeeded, want conflict")
	}
	if len(launcher.calls) != 0 {
		t.Errorf("launchctl was invoked after conflict: %q", launcher.calls)
	}
}

func TestUninstallRefusesConflictingLaunchAgent(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	if err := os.WriteFile(paths.Plist, []byte("other service"), 0o644); err != nil {
		t.Fatalf("write conflicting plist: %v", err)
	}
	launcher := &fakeLauncher{fail: map[string]error{}}
	manager := NewManager(paths, 501, launcher)
	if err := manager.Uninstall(context.Background()); err == nil {
		t.Fatal("uninstall succeeded, want conflict")
	}
	if len(launcher.calls) != 0 {
		t.Errorf("launchctl was invoked after conflict: %q", launcher.calls)
	}
	if _, err := os.Stat(paths.Plist); err != nil {
		t.Errorf("conflicting plist was removed: %v", err)
	}
}

func TestLifecycleCommandFailuresAreReportedWithoutDeletingRuntime(t *testing.T) {
	home := t.TempDir()
	paths := PathsForHome(home)
	if err := os.MkdirAll(filepath.Dir(paths.Plist), 0o700); err != nil {
		t.Fatalf("create LaunchAgents directory: %v", err)
	}
	manager := NewManager(paths, 501, &fakeLauncher{
		fail: map[string]error{
			"bootstrap gui/501 " + paths.Plist: errors.New("bootstrap denied"),
		},
	})
	if err := manager.Start(context.Background()); err == nil {
		t.Fatal("start succeeded despite bootstrap failure")
	}
	if err := manager.Restart(context.Background()); err == nil {
		t.Fatal("restart succeeded despite bootstrap failure")
	}

	source := filepath.Join(t.TempDir(), "missing-autoboard")
	manager = NewManager(paths, 501, &fakeLauncher{fail: map[string]error{}})
	if err := manager.Install(context.Background(), source); err == nil {
		t.Fatal("install succeeded with missing source executable")
	}

	if err := os.WriteFile(paths.Plist, manager.plist(), 0o644); err != nil {
		t.Fatalf("write managed plist: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Executable), 0o700); err != nil {
		t.Fatalf("create executable directory: %v", err)
	}
	if err := os.WriteFile(paths.Executable, []byte("binary"), 0o700); err != nil {
		t.Fatalf("write managed executable: %v", err)
	}
	launcher := &fakeLauncher{
		fail: map[string]error{
			"bootout gui/501/" + Label: errors.New("bootout denied"),
		},
		loaded: true,
	}
	manager = NewManager(paths, 501, launcher)
	if err := manager.Stop(context.Background()); err == nil {
		t.Fatal("stop succeeded despite bootout failure")
	}
	if err := manager.Restart(context.Background()); err == nil {
		t.Fatal("restart succeeded despite bootout failure")
	}
	if err := manager.Uninstall(context.Background()); err == nil {
		t.Fatal("uninstall succeeded despite bootout failure")
	}
	if _, err := os.Stat(paths.Executable); err != nil {
		t.Errorf("runtime was removed after failed bootout: %v", err)
	}
}

func TestAtomicFileHelpersReportInvalidPaths(t *testing.T) {
	root := t.TempDir()
	if err := copyAtomic(
		filepath.Join(root, "missing"),
		filepath.Join(root, "target"),
		0o600,
	); err == nil {
		t.Fatal("copyAtomic succeeded with missing source")
	}
	missingDirectory := filepath.Join(root, "missing-directory")
	if err := writeAtomic(
		filepath.Join(missingDirectory, "target"),
		[]byte("content"),
		0o600,
	); err == nil {
		t.Fatal("writeAtomic succeeded with missing parent directory")
	}
}
