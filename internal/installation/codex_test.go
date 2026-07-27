package installation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeRunner struct {
	registrations []codexRegistration
	explicit      bool
	calls         [][]string
	fail          map[string]error
	configPath    string
}

func (f *fakeRunner) Output(
	_ context.Context,
	name string,
	arguments ...string,
) ([]byte, error) {
	call := append([]string{name}, arguments...)
	f.calls = append(f.calls, call)
	key := strings.Join(call, " ")
	if err := f.fail[key]; err != nil {
		return nil, err
	}
	if key == "codex mcp list --json" {
		if !f.explicit && f.configPath != "" {
			content, err := os.ReadFile(f.configPath)
			if err != nil {
				return nil, err
			}
			if strings.Contains(
				string(content),
				"[mcp_servers.autoboard]",
			) {
				var registration codexRegistration
				registration.Name = MCPName
				registration.Transport.Type = "streamable_http"
				registration.Transport.URL = MCPURL
				return json.Marshal([]codexRegistration{registration})
			}
			return []byte("[]"), nil
		}
		return json.Marshal(f.registrations)
	}
	return nil, nil
}

func TestEnsureRegistersExactEndpointAndNarrowlySetsApproval(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	original := "[model]\nname = \"gpt\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runner := &fakeRunner{
		fail:       map[string]error{},
		configPath: configPath,
	}
	manager := CodexManager{Runner: runner, ConfigPath: configPath}
	added, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !added {
		t.Fatal("ensure did not report new registration")
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(content)
	for _, expected := range []string{
		original,
		"[mcp_servers.autoboard]",
		`url = "` + MCPURL + `"`,
		`default_tools_approval_mode = "writes"`,
		"required = false",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("config missing %q:\n%s", expected, got)
		}
	}
	added, err = manager.Ensure(context.Background())
	if err != nil || added {
		t.Errorf("second ensure = added %v, error %v", added, err)
	}
	mutationCalls := 0
	for _, call := range runner.calls {
		if slices.Contains(call, "add") || slices.Contains(call, "remove") {
			mutationCalls++
		}
	}
	if mutationCalls != 0 {
		t.Errorf("Codex mutation calls = %d, want 0", mutationCalls)
	}
	if err := manager.Remove(context.Background()); err != nil {
		t.Fatalf("remove: %v", err)
	}
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(restored) != original {
		t.Errorf("restored config differs:\n%s", restored)
	}
}

func TestValidateRefusesConflictingRegistration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var registration codexRegistration
	registration.Name = MCPName
	registration.Transport.URL = "http://127.0.0.1:9999/mcp"
	runner := &fakeRunner{
		registrations: []codexRegistration{registration},
		explicit:      true,
		fail:          map[string]error{},
		configPath:    configPath,
	}
	manager := CodexManager{Runner: runner, ConfigPath: configPath}
	if _, err := manager.Validate(context.Background()); err == nil {
		t.Fatal("validate succeeded, want conflict")
	}
}

func TestEnsureRefusesDuplicateAutoboardTables(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(
		configPath,
		[]byte(
			"[mcp_servers.autoboard]\nurl = \"old-one\"\n\n"+
				"[mcp_servers.autoboard]\nurl = \"old-two\"\n",
		),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runner := &fakeRunner{
		fail:       map[string]error{},
		configPath: configPath,
	}
	manager := CodexManager{Runner: runner, ConfigPath: configPath}
	_, err := manager.Ensure(context.Background())
	if err == nil {
		t.Fatal("ensure succeeded, want configuration failure")
	}
}

func TestConfigMutationRefusesAlternateAutoboardTableSyntax(t *testing.T) {
	for _, header := range []string{
		`[mcp_servers."autoboard"]`,
		`["mcp_servers".autoboard]`,
		`['mcp_servers'.'autoboard']`,
		`[ mcp_servers . autoboard ]`,
		`[mcp_servers."auto\u0062oard"]`,
	} {
		t.Run(header, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.toml")
			original := header + "\nurl = \"" + MCPURL + "\"\n"
			if err := os.WriteFile(
				configPath,
				[]byte(original),
				0o600,
			); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if err := ensureAutoboardConfig(configPath); err == nil {
				t.Fatal("ensure succeeded, want alternate syntax refusal")
			}
			if err := removeAutoboardConfig(configPath); err == nil {
				t.Fatal("remove succeeded, want alternate syntax refusal")
			}
			content, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			if string(content) != original {
				t.Errorf("config mutated:\n%s", content)
			}
		})
	}
}

func TestStatusPropagatesCodexFailure(t *testing.T) {
	runner := &fakeRunner{
		fail: map[string]error{
			"codex mcp list --json": errors.New("codex missing"),
		},
	}
	_, err := (CodexManager{Runner: runner}).Status(context.Background())
	if err == nil {
		t.Fatal("status succeeded, want failure")
	}
}
