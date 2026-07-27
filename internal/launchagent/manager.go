package launchagent

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const Label = "com.autoboard.server"

type Launcher interface {
	Run(context.Context, ...string) error
}

type Paths struct {
	DataDir       string
	Executable    string
	Plist         string
	InstallRecord string
	StandardOut   string
	StandardError string
}

func PathsForHome(home string) Paths {
	dataDir := filepath.Join(
		home,
		"Library",
		"Application Support",
		"Autoboard",
	)
	return Paths{
		DataDir:       dataDir,
		Executable:    filepath.Join(dataDir, "bin", "autoboard"),
		Plist:         filepath.Join(home, "Library", "LaunchAgents", Label+".plist"),
		InstallRecord: filepath.Join(dataDir, "install.json"),
		StandardOut:   filepath.Join(dataDir, "logs", "autoboard.log"),
		StandardError: filepath.Join(dataDir, "logs", "autoboard.error.log"),
	}
}

type Manager struct {
	paths    Paths
	uid      int
	launcher Launcher
}

func NewManager(paths Paths, uid int, launcher Launcher) *Manager {
	return &Manager{paths: paths, uid: uid, launcher: launcher}
}

func (m *Manager) Install(
	ctx context.Context,
	sourceExecutable string,
) error {
	return m.InstallVerified(ctx, sourceExecutable, nil)
}

func (m *Manager) InstallVerified(
	ctx context.Context,
	sourceExecutable string,
	verify func(context.Context) error,
) error {
	if err := os.MkdirAll(m.paths.DataDir, 0o700); err != nil {
		return fmt.Errorf("create Autoboard data directory: %w", err)
	}
	if err := os.Chmod(m.paths.DataDir, 0o700); err != nil {
		return fmt.Errorf("protect Autoboard data directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.paths.Executable), 0o700); err != nil {
		return fmt.Errorf("create Autoboard binary directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.paths.StandardOut), 0o700); err != nil {
		return fmt.Errorf("create Autoboard log directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.paths.Plist), 0o755); err != nil {
		return fmt.Errorf("create user LaunchAgents directory: %w", err)
	}
	plistContent := m.plist()
	currentPlist, plistErr := os.ReadFile(m.paths.Plist)
	plistExists := plistErr == nil
	if plistExists && !bytes.Equal(currentPlist, plistContent) {
		return fmt.Errorf(
			"refuse to replace conflicting LaunchAgent at %s",
			m.paths.Plist,
		)
	} else if plistErr != nil && !errors.Is(plistErr, os.ErrNotExist) {
		return fmt.Errorf(
			"read existing Autoboard LaunchAgent: %w",
			plistErr,
		)
	}
	if _, err := os.Stat(m.paths.Executable); err == nil && !plistExists {
		return fmt.Errorf(
			"refuse to replace unowned executable at %s",
			m.paths.Executable,
		)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing Autoboard executable: %w", err)
	}
	previousExecutable, err := snapshotFile(m.paths.Executable)
	if err != nil {
		return fmt.Errorf("snapshot Autoboard executable: %w", err)
	}
	previousPlist, err := snapshotFile(m.paths.Plist)
	if err != nil {
		return fmt.Errorf("snapshot Autoboard LaunchAgent: %w", err)
	}
	wasLoaded, err := m.Loaded(ctx)
	if err != nil {
		return err
	}
	if wasLoaded {
		if err := m.launcher.Run(ctx, "bootout", m.target()); err != nil {
			return fmt.Errorf("stop existing Autoboard LaunchAgent: %w", err)
		}
	}
	if err := copyAtomic(sourceExecutable, m.paths.Executable, 0o755); err != nil {
		_ = m.restore(ctx, previousExecutable, previousPlist, wasLoaded)
		return fmt.Errorf("install Autoboard executable: %w", err)
	}
	if err := writeAtomic(m.paths.Plist, plistContent, 0o644); err != nil {
		_ = m.restore(ctx, previousExecutable, previousPlist, wasLoaded)
		return fmt.Errorf("write Autoboard LaunchAgent: %w", err)
	}
	if err := m.bootstrap(ctx); err != nil {
		rollbackErr := m.restore(
			ctx,
			previousExecutable,
			previousPlist,
			wasLoaded,
		)
		return errors.Join(
			fmt.Errorf("bootstrap Autoboard LaunchAgent: %w", err),
			rollbackErr,
		)
	}
	if verify != nil {
		if err := verify(ctx); err != nil {
			rollbackErr := m.restore(
				ctx,
				previousExecutable,
				previousPlist,
				wasLoaded,
			)
			return errors.Join(
				fmt.Errorf("verify Autoboard installation: %w", err),
				rollbackErr,
			)
		}
	}
	return nil
}

func (m *Manager) Uninstall(ctx context.Context) error {
	if current, err := os.ReadFile(m.paths.Plist); err == nil {
		if !bytes.Equal(current, m.plist()) {
			return fmt.Errorf(
				"refuse to remove conflicting LaunchAgent at %s",
				m.paths.Plist,
			)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing Autoboard LaunchAgent: %w", err)
	}
	loaded, err := m.Loaded(ctx)
	if err != nil {
		return err
	}
	if loaded {
		if err := m.launcher.Run(ctx, "bootout", m.target()); err != nil {
			return fmt.Errorf("stop Autoboard LaunchAgent: %w", err)
		}
	}
	var failures []error
	for _, target := range []string{
		m.paths.Plist,
		m.paths.Executable,
		m.paths.InstallRecord,
	} {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove %s: %w", target, err))
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) Start(ctx context.Context) error {
	loaded, err := m.Loaded(ctx)
	if err != nil {
		return err
	}
	if loaded {
		return nil
	}
	if err := m.bootstrap(ctx); err != nil {
		return fmt.Errorf("start Autoboard LaunchAgent: %w", err)
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	loaded, err := m.Loaded(ctx)
	if err != nil {
		return err
	}
	if !loaded {
		return nil
	}
	if err := m.launcher.Run(ctx, "bootout", m.target()); err != nil {
		return fmt.Errorf("stop Autoboard LaunchAgent: %w", err)
	}
	return nil
}

func (m *Manager) Restart(ctx context.Context) error {
	loaded, err := m.Loaded(ctx)
	if err != nil {
		return err
	}
	if loaded {
		if err := m.launcher.Run(ctx, "bootout", m.target()); err != nil {
			return fmt.Errorf("stop Autoboard LaunchAgent: %w", err)
		}
	}
	if err := m.bootstrap(ctx); err != nil {
		return fmt.Errorf("restart Autoboard LaunchAgent: %w", err)
	}
	return nil
}

func (m *Manager) Loaded(ctx context.Context) (bool, error) {
	if probe, ok := m.launcher.(interface {
		Loaded(context.Context, ...string) (bool, error)
	}); ok {
		loaded, err := probe.Loaded(ctx, "print", m.target())
		if err != nil {
			return false, fmt.Errorf(
				"inspect Autoboard LaunchAgent state: %w",
				err,
			)
		}
		return loaded, nil
	}
	if err := m.launcher.Run(ctx, "print", m.target()); err != nil {
		return false, fmt.Errorf(
			"inspect Autoboard LaunchAgent state: %w",
			err,
		)
	}
	return true, nil
}

func (m *Manager) bootstrap(ctx context.Context) error {
	var err error
	for range 10 {
		arguments := []string{"bootstrap", m.domain(), m.paths.Plist}
		if bootstrapper, ok := m.launcher.(interface {
			Bootstrap(context.Context, ...string) error
		}); ok {
			err = bootstrapper.Bootstrap(ctx, arguments...)
		} else {
			err = m.launcher.Run(ctx, arguments...)
		}
		if err == nil || !strings.Contains(err.Error(), "exit status 5") {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

type fileSnapshot struct {
	exists  bool
	content []byte
	mode    os.FileMode
	path    string
}

func snapshotFile(path string) (fileSnapshot, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{
		exists:  true,
		content: content,
		mode:    info.Mode().Perm(),
		path:    path,
	}, nil
}

func (m *Manager) restore(
	ctx context.Context,
	executable fileSnapshot,
	plist fileSnapshot,
	wasLoaded bool,
) error {
	_ = m.launcher.Run(ctx, "bootout", m.target())
	var failures []error
	for _, snapshot := range []fileSnapshot{executable, plist} {
		if snapshot.exists {
			if err := writeAtomic(
				snapshot.path,
				snapshot.content,
				snapshot.mode,
			); err != nil {
				failures = append(failures, err)
			}
		} else if err := os.Remove(snapshot.path); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	if wasLoaded && len(failures) == 0 {
		if err := m.bootstrap(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) Status(ctx context.Context) error {
	loaded, err := m.Loaded(ctx)
	if err != nil {
		return err
	}
	if !loaded {
		return errors.New("autoboard LaunchAgent is not loaded")
	}
	return nil
}

func (m *Manager) domain() string {
	return fmt.Sprintf("gui/%d", m.uid)
}

func (m *Manager) target() string {
	return m.domain() + "/" + Label
}

func (m *Manager) plist() []byte {
	values := map[string]string{
		"label":      Label,
		"executable": m.paths.Executable,
		"stdout":     m.paths.StandardOut,
		"stderr":     m.paths.StandardError,
	}
	for key, value := range values {
		values[key] = escapeXML(value)
	}
	return fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>ThrottleInterval</key>
  <integer>5</integer>
  <key>Umask</key>
  <integer>63</integer>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, values["label"], values["executable"], values["stdout"], values["stderr"])
}

func escapeXML(value string) string {
	var encoded bytes.Buffer
	if err := xml.EscapeText(&encoded, []byte(value)); err != nil {
		panic(err)
	}
	return encoded.String()
}

func copyAtomic(source string, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(target), ".autoboard-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, input); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
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
	return os.Rename(temporaryPath, target)
}

func writeAtomic(target string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".autoboard-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
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
	return os.Rename(temporaryPath, target)
}
