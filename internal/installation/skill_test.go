package installation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillManagerValidateAcceptsValidSource(t *testing.T) {
	source := writeSkillSource(t)
	manager := SkillManager{SourceDir: source, DestinationDir: filepath.Join(t.TempDir(), "autoboard")}

	if err := manager.Validate(); err != nil {
		t.Fatalf("validate valid source: %v", err)
	}
}

func TestSkillManagerValidateRejectsInvalidSource(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, source string){
		"missing required metadata": func(t *testing.T, source string) {
			t.Helper()
			if err := os.Remove(filepath.Join(source, "agents", "openai.yaml")); err != nil {
				t.Fatalf("remove metadata: %v", err)
			}
		},
		"wrong skill name": func(t *testing.T, source string) {
			t.Helper()
			writeFile(t, filepath.Join(source, "SKILL.md"), []byte("---\nname: another-skill\ndescription: Use Autoboard to inspect and manage the local project board.\n---\n\n<!-- autoboard.codex-integration.v1 -->\n"))
		},
		"wrong MCP dependency": func(t *testing.T, source string) {
			t.Helper()
			writeFile(t, filepath.Join(source, "agents", "openai.yaml"), []byte(validOpenAIYAML("another-server")))
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := writeSkillSource(t)
			mutate(t, source)
			manager := SkillManager{SourceDir: source, DestinationDir: filepath.Join(t.TempDir(), "autoboard")}
			if err := manager.Validate(); err == nil {
				t.Fatal("validate succeeded, want invalid source error")
			}
		})
	}
}

func TestSkillManagerStatusReportsSkillStates(t *testing.T) {
	source := writeSkillSource(t)
	for name, setup := range map[string]struct {
		setup func(t *testing.T, destination string)
		want  SkillStatus
	}{
		"missing destination": {want: SkillMissing},
		"exact copied content": {
			setup: copySkillSource,
			want:  SkillCurrent,
		},
		"marker owned outdated content": {
			setup: func(t *testing.T, destination string) {
				t.Helper()
				copySkillSource(t, destination)
				writeFile(t, filepath.Join(destination, "agents", "openai.yaml"), []byte(validOpenAIYAML("autoboard")+"# outdated\n"))
			},
			want: SkillOutdated,
		},
		"marker owned skill missing canonical metadata": {
			setup: func(t *testing.T, destination string) {
				t.Helper()
				copySkillSource(t, destination)
				if err := os.Remove(filepath.Join(destination, "agents", "openai.yaml")); err != nil {
					t.Fatalf("remove destination metadata: %v", err)
				}
			},
			want: SkillOutdated,
		},
		"conflicting file": {
			setup: func(t *testing.T, destination string) {
				t.Helper()
				writeFile(t, destination, []byte("not a skill"))
			},
			want: SkillConflicting,
		},
		"conflicting directory": {
			setup: func(t *testing.T, destination string) {
				t.Helper()
				if err := os.MkdirAll(destination, 0o700); err != nil {
					t.Fatalf("make destination: %v", err)
				}
				writeFile(t, filepath.Join(destination, "SKILL.md"), []byte("unowned"))
			},
			want: SkillConflicting,
		},
		"conflicting symlink": {
			setup: func(t *testing.T, destination string) {
				t.Helper()
				if err := os.Symlink(t.TempDir(), destination); err != nil {
					t.Fatalf("make symlink: %v", err)
				}
			},
			want: SkillConflicting,
		},
		"conflicting intermediate symlink": {
			setup: func(t *testing.T, destination string) {
				t.Helper()
				copySkillSource(t, destination)
				if err := os.RemoveAll(filepath.Join(destination, "agents")); err != nil {
					t.Fatalf("remove destination agents directory: %v", err)
				}
				target := filepath.Join(t.TempDir(), "agents")
				writeFile(t, filepath.Join(target, "openai.yaml"), []byte(validOpenAIYAML("autoboard")))
				if err := os.Symlink(target, filepath.Join(destination, "agents")); err != nil {
					t.Fatalf("make intermediate symlink: %v", err)
				}
			},
			want: SkillConflicting,
		},
	} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "autoboard")
			if setup.setup != nil {
				setup.setup(t, destination)
			}
			manager := SkillManager{SourceDir: source, DestinationDir: destination}
			got, err := manager.Status()
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if got != setup.want {
				t.Errorf("status = %q, want %q", got, setup.want)
			}
		})
	}
}

func TestSkillManagerEnsureInstallsAndIsIdempotent(t *testing.T) {
	source := writeSkillSource(t)
	destination := filepath.Join(t.TempDir(), "nested", "autoboard")
	manager := SkillManager{SourceDir: source, DestinationDir: destination}

	created, err := manager.Ensure()
	if err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	if !created {
		t.Fatal("initial ensure did not report creation")
	}
	assertSkillMatchesSource(t, source, destination)

	created, err = manager.Ensure()
	if err != nil {
		t.Fatalf("repeated ensure: %v", err)
	}
	if created {
		t.Fatal("repeated ensure reported creation")
	}
}

func TestSkillManagerEnsureRefreshesOwnedOutdatedSkill(t *testing.T) {
	source := writeSkillSource(t)
	destination := filepath.Join(t.TempDir(), "autoboard")
	copySkillSource(t, destination)
	writeFile(t, filepath.Join(destination, "SKILL.md"), []byte(validSkillMarkdown()+"\nchanged\n"))
	manager := SkillManager{SourceDir: source, DestinationDir: destination}

	created, err := manager.Ensure()
	if err != nil {
		t.Fatalf("refresh ensure: %v", err)
	}
	if created {
		t.Fatal("refresh ensure reported creation")
	}
	assertSkillMatchesSource(t, source, destination)
}

func TestSkillManagerEnsureRestoresOwnedSkillWhenReplacementFails(t *testing.T) {
	source := writeSkillSource(t)
	destination := filepath.Join(t.TempDir(), "autoboard")
	copySkillSource(t, destination)
	original := []byte(validSkillMarkdown() + "\nolder copy\n")
	writeFile(t, filepath.Join(destination, "SKILL.md"), original)
	manager := SkillManager{SourceDir: source, DestinationDir: destination}

	originalRename := skillRename
	t.Cleanup(func() { skillRename = originalRename })
	failReplacement := true
	skillRename = func(oldPath, newPath string) error {
		if failReplacement && newPath == destination {
			failReplacement = false
			return errors.New("simulated replacement failure")
		}
		return originalRename(oldPath, newPath)
	}
	if _, err := manager.Ensure(); err == nil {
		t.Fatal("ensure succeeded, want replacement failure")
	}
	content, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	if err != nil {
		t.Fatalf("read restored skill: %v", err)
	}
	if string(content) != string(original) {
		t.Errorf("restored skill = %q, want %q", content, original)
	}
}

func TestSkillManagerRemoveIsOwnershipSafeAndIdempotent(t *testing.T) {
	source := writeSkillSource(t)
	t.Run("absent skill", func(t *testing.T) {
		manager := SkillManager{SourceDir: source, DestinationDir: filepath.Join(t.TempDir(), "autoboard")}
		if err := manager.Remove(); err != nil {
			t.Fatalf("remove absent skill: %v", err)
		}
	})
	t.Run("owned skill", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "autoboard")
		copySkillSource(t, destination)
		manager := SkillManager{SourceDir: source, DestinationDir: destination}
		if err := manager.Remove(); err != nil {
			t.Fatalf("remove owned skill: %v", err)
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("removed destination lstat error = %v, want not exist", err)
		}
	})
	t.Run("owned skill with unavailable source", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "autoboard")
		copySkillSource(t, destination)
		manager := SkillManager{
			SourceDir:      filepath.Join(t.TempDir(), "missing-source"),
			DestinationDir: destination,
		}
		if err := manager.Remove(); err != nil {
			t.Fatalf("remove owned skill with unavailable source: %v", err)
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("removed destination lstat error = %v, want not exist", err)
		}
	})
	t.Run("conflicting skill", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "autoboard")
		writeFile(t, destination, []byte("not owned"))
		manager := SkillManager{SourceDir: source, DestinationDir: destination}
		if err := manager.Remove(); err == nil {
			t.Fatal("remove succeeded, want conflict refusal")
		}
	})
}

func writeSkillSource(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	writeFile(t, filepath.Join(source, "SKILL.md"), []byte(validSkillMarkdown()))
	writeFile(t, filepath.Join(source, "agents", "openai.yaml"), []byte(validOpenAIYAML("autoboard")))
	return source
}

func copySkillSource(t *testing.T, destination string) {
	t.Helper()
	writeFile(t, filepath.Join(destination, "SKILL.md"), []byte(validSkillMarkdown()))
	writeFile(t, filepath.Join(destination, "agents", "openai.yaml"), []byte(validOpenAIYAML("autoboard")))
}

func assertSkillMatchesSource(t *testing.T, source, destination string) {
	t.Helper()
	for _, name := range []string{"SKILL.md", filepath.Join("agents", "openai.yaml")} {
		want, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatalf("read source %s: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil {
			t.Fatalf("read destination %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("destination %s does not match source", name)
		}
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func validSkillMarkdown() string {
	return "---\nname: autoboard\ndescription: Use Autoboard to inspect and manage the local project board.\n---\n\n<!-- autoboard.codex-integration.v1 -->\n"
}

func validOpenAIYAML(server string) string {
	return "interface:\n  display_name: \"Autoboard\"\n  short_description: \"Inspect and manage the local Autoboard project board\"\n\npolicy:\n  allow_implicit_invocation: true\n\ndependencies:\n  tools:\n    - type: \"mcp\"\n      value: \"" + server + "\"\n      description: \"Autoboard local project-board tools\"\n      transport: \"streamable_http\"\n      url: \"http://127.0.0.1:4040/mcp\"\n"
}
