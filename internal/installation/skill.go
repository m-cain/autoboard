package installation

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

const (
	skillMarker = "<!-- autoboard.codex-integration.v1 -->"
	skillName   = "autoboard"
	skillURL    = "http://127.0.0.1:4040/mcp"
)

var (
	skillRename          = os.Rename
	skillSwapDirectories = atomicSwapSkillDirectories
)

type SkillStatus string

const (
	SkillMissing     SkillStatus = "missing"
	SkillCurrent     SkillStatus = "current"
	SkillOutdated    SkillStatus = "outdated"
	SkillConflicting SkillStatus = "conflicting"
)

type SkillManager struct {
	SourceDir      string
	DestinationDir string
}

type skillMetadata struct {
	Interface struct {
		DisplayName      string `yaml:"display_name"`
		ShortDescription string `yaml:"short_description"`
	} `yaml:"interface"`
	Policy struct {
		AllowImplicitInvocation bool `yaml:"allow_implicit_invocation"`
	} `yaml:"policy"`
	Dependencies struct {
		Tools []struct {
			Type        string `yaml:"type"`
			Value       string `yaml:"value"`
			Description string `yaml:"description"`
			Transport   string `yaml:"transport"`
			URL         string `yaml:"url"`
		} `yaml:"tools"`
	} `yaml:"dependencies"`
}

func (m SkillManager) Status() (SkillStatus, error) {
	info, err := os.Lstat(m.DestinationDir)
	if errors.Is(err, os.ErrNotExist) {
		return SkillMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect installed skill: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return SkillConflicting, nil
	}
	owned, err := skillDirectoryOwned(m.DestinationDir)
	if err != nil {
		return "", err
	}
	if !owned {
		return SkillConflicting, nil
	}
	conflicting, err := hasUnsafeSkillPath(m.DestinationDir)
	if err != nil {
		return "", err
	}
	if conflicting {
		return SkillConflicting, nil
	}
	current, err := m.matchesSource()
	if err != nil {
		return "", err
	}
	if current {
		return SkillCurrent, nil
	}
	return SkillOutdated, nil
}

func (m SkillManager) Validate() error {
	if err := validateSkillSource(m.SourceDir); err != nil {
		return err
	}
	status, err := m.Status()
	if err != nil {
		return err
	}
	if status == SkillConflicting {
		return fmt.Errorf("refuse to replace conflicting skill destination %q", m.DestinationDir)
	}
	return nil
}

func (m SkillManager) Ensure() (bool, error) {
	if err := m.Validate(); err != nil {
		return false, err
	}
	status, err := m.Status()
	if err != nil {
		return false, err
	}
	if status == SkillCurrent {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(m.DestinationDir), 0o700); err != nil {
		return false, fmt.Errorf("create skill parent directory: %w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(m.DestinationDir), ".autoboard-skill-")
	if err != nil {
		return false, fmt.Errorf("create staged skill directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := os.Chmod(stage, 0o700); err != nil {
		return false, fmt.Errorf("set staged skill directory permissions: %w", err)
	}
	if err := copySkillFiles(m.SourceDir, stage); err != nil {
		return false, err
	}
	if status == SkillMissing {
		if err := skillRename(stage, m.DestinationDir); err != nil {
			return false, fmt.Errorf("install staged skill: %w", err)
		}
		return true, nil
	}

	if err := skillSwapDirectories(stage, m.DestinationDir); err != nil {
		return false, fmt.Errorf("replace skill: %w", err)
	}
	return false, nil
}

func (m SkillManager) Remove() error {
	info, err := os.Lstat(m.DestinationDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect installed skill: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refuse to remove conflicting skill destination %q", m.DestinationDir)
	}
	owned, err := skillDirectoryOwned(m.DestinationDir)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("refuse to remove conflicting skill destination %q", m.DestinationDir)
	}
	if err := os.RemoveAll(m.DestinationDir); err != nil {
		return fmt.Errorf("remove installed skill: %w", err)
	}
	return nil
}

func validateSkillSource(source string) error {
	skill, err := readSkillFile(source, "SKILL.md")
	if err != nil {
		return err
	}
	if !hasSkillFrontmatter(skill) {
		return errors.New("skill source has invalid Autoboard skill metadata")
	}
	metadataContent, err := readSkillFile(source, filepath.Join("agents", "openai.yaml"))
	if err != nil {
		return err
	}
	var metadata skillMetadata
	if err := yaml.Unmarshal(metadataContent, &metadata); err != nil {
		return fmt.Errorf("decode skill metadata: %w", err)
	}
	if metadata.Interface.DisplayName != "Autoboard" ||
		metadata.Interface.ShortDescription != "Inspect and manage the local Autoboard project board" ||
		!metadata.Policy.AllowImplicitInvocation ||
		len(metadata.Dependencies.Tools) != 1 {
		return errors.New("skill source has invalid Autoboard metadata")
	}
	tool := metadata.Dependencies.Tools[0]
	if tool.Type != "mcp" || tool.Value != skillName ||
		tool.Description != "Autoboard local project-board tools" ||
		tool.Transport != "streamable_http" || tool.URL != skillURL {
		return errors.New("skill source has invalid Autoboard MCP dependency")
	}
	return nil
}

func (m SkillManager) matchesSource() (bool, error) {
	for _, relativePath := range []string{"SKILL.md", filepath.Join("agents", "openai.yaml")} {
		source, err := readSkillFile(m.SourceDir, relativePath)
		if err != nil {
			return false, err
		}
		destination, err := readSkillFile(m.DestinationDir, relativePath)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !bytes.Equal(source, destination) {
			return false, nil
		}
	}
	return true, nil
}

func skillDirectoryOwned(directory string) (bool, error) {
	skill, err := readSkillFile(directory, "SKILL.md")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for line := range strings.Lines(string(skill)) {
		if strings.TrimSuffix(line, "\n") == skillMarker {
			return true, nil
		}
	}
	return false, nil
}

func hasSkillFrontmatter(content []byte) bool {
	lines := strings.Split(string(content), "\n")
	if len(lines) < 5 || lines[0] != "---" {
		return false
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" {
			end = index
			break
		}
	}
	if end == -1 {
		return false
	}
	frontmatter := strings.Join(lines[1:end], "\n")
	return strings.Contains(frontmatter, "name: "+skillName+"\n") &&
		strings.Contains(frontmatter, "description: Use Autoboard to inspect and manage the local project board.") &&
		hasStandaloneMarker(content)
}

func hasStandaloneMarker(content []byte) bool {
	for line := range strings.Lines(string(content)) {
		if strings.TrimSuffix(line, "\n") == skillMarker {
			return true
		}
	}
	return false
}

func readSkillFile(directory, relativePath string) ([]byte, error) {
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return nil, fmt.Errorf("inspect skill directory: %w", err)
	}
	if directoryInfo.Mode()&fs.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return nil, errors.New("skill directory is not a real directory")
	}
	path := directory
	components := strings.Split(relativePath, string(filepath.Separator))
	for index, component := range components {
		path = filepath.Join(path, component)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("read skill file %s: %w", relativePath, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("skill file %s has a symlinked path component", relativePath)
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, fmt.Errorf("skill file %s has a non-directory path component", relativePath)
		}
		if index == len(components)-1 && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("skill file %s is not a regular file", relativePath)
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file %s: %w", relativePath, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("skill file %s is not a regular file", relativePath)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file %s: %w", relativePath, err)
	}
	return content, nil
}

func hasUnsafeSkillPath(directory string) (bool, error) {
	for _, relativePath := range []string{"SKILL.md", filepath.Join("agents", "openai.yaml")} {
		path := directory
		components := strings.Split(relativePath, string(filepath.Separator))
		for index, component := range components {
			path = filepath.Join(path, component)
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil {
				return false, fmt.Errorf("inspect installed skill file %s: %w", relativePath, err)
			}
			if info.Mode()&fs.ModeSymlink != 0 ||
				(index < len(components)-1 && !info.IsDir()) ||
				(index == len(components)-1 && !info.Mode().IsRegular()) {
				return true, nil
			}
		}
	}
	return false, nil
}

func copySkillFiles(source, destination string) error {
	for _, relativePath := range []string{"SKILL.md", filepath.Join("agents", "openai.yaml")} {
		content, err := readSkillFile(source, relativePath)
		if err != nil {
			return err
		}
		path := filepath.Join(destination, relativePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create staged skill directory: %w", err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return fmt.Errorf("write staged skill file: %w", err)
		}
	}
	return nil
}
