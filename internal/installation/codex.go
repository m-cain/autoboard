package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	MCPName = "autoboard"
	MCPURL  = "http://127.0.0.1:4040/mcp"
)

type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type CommandRunner struct{}

func (CommandRunner) Output(
	ctx context.Context,
	name string,
	arguments ...string,
) ([]byte, error) {
	//nolint:gosec // The command name is supplied by the narrowly scoped Runner abstraction.
	command := exec.CommandContext(ctx, name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf(
			"%s %s: %w: %s",
			name,
			strings.Join(arguments, " "),
			err,
			strings.TrimSpace(string(output)),
		)
	}
	return output, nil
}

type CodexManager struct {
	Runner     Runner
	ConfigPath string
	SkillPath  string
}

type CodexStatus struct {
	Registered bool
	URL        string
}

type codexRegistration struct {
	Name      string `json:"name"`
	Transport struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"transport"`
}

func (m CodexManager) Validate(ctx context.Context) (CodexStatus, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return CodexStatus{}, err
	}
	if status.Registered && status.URL != MCPURL {
		return status, fmt.Errorf(
			"refuse to replace conflicting Codex MCP registration %q (%s)",
			MCPName,
			status.URL,
		)
	}
	return status, nil
}

func (m CodexManager) Ensure(ctx context.Context) (bool, error) {
	status, err := m.Validate(ctx)
	if err != nil {
		return false, err
	}
	added := !status.Registered

	if err := ensureAutoboardConfig(m.ConfigPath); err != nil {
		return false, err
	}
	verified, err := m.Status(ctx)
	if err != nil {
		return false, err
	}
	if !verified.Registered || verified.URL != MCPURL {
		return false, errors.New("codex did not read back the Autoboard MCP registration")
	}
	return added, nil
}

func (m CodexManager) Remove(ctx context.Context) error {
	status, err := m.Validate(ctx)
	if err != nil {
		return err
	}
	if !status.Registered {
		return nil
	}
	if err := removeAutoboardConfig(m.ConfigPath); err != nil {
		return err
	}
	verified, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if verified.Registered {
		return errors.New("codex still reports the Autoboard MCP registration")
	}
	return nil
}

func (m CodexManager) Status(ctx context.Context) (CodexStatus, error) {
	output, err := m.runner().Output(ctx, "codex", "mcp", "list", "--json")
	if err != nil {
		return CodexStatus{}, fmt.Errorf("list Codex MCP registrations: %w", err)
	}
	var registrations []codexRegistration
	if err := json.Unmarshal(output, &registrations); err != nil {
		return CodexStatus{}, fmt.Errorf(
			"decode Codex MCP registrations: %w",
			err,
		)
	}
	for _, registration := range registrations {
		if registration.Name == MCPName {
			return CodexStatus{
				Registered: true,
				URL:        registration.Transport.URL,
			}, nil
		}
	}
	return CodexStatus{}, nil
}

func (m CodexManager) runner() Runner {
	if m.Runner != nil {
		return m.Runner
	}
	return CommandRunner{}
}

func (m CodexManager) SkillManager(sourceDir string) SkillManager {
	return SkillManager{
		SourceDir:      sourceDir,
		DestinationDir: m.SkillPath,
	}
}

func (m CodexManager) RemoveSkill() error {
	return m.SkillManager("").Remove()
}

func ensureAutoboardConfig(path string) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create Codex configuration directory: %w", err)
		}
		content = nil
	} else if err != nil {
		return fmt.Errorf("read Codex configuration: %w", err)
	}
	header := "[mcp_servers." + MCPName + "]"
	tableCount := autoboardTableCount(content)
	if tableCount > 1 {
		return errors.New("codex configuration has duplicate Autoboard MCP tables")
	}
	start, end, err := tableRange(content, header)
	if err != nil {
		return err
	}
	if tableCount == 1 && start == -1 {
		return errors.New("refuse to modify Autoboard MCP table using alternate TOML syntax")
	}
	table := fmt.Appendf(nil,
		"%s\nurl = %q\ndefault_tools_approval_mode = \"writes\"\nrequired = false\n",
		header,
		MCPURL,
	)
	if start == -1 {
		separator := []byte("\n")
		if len(content) == 0 || bytes.HasSuffix(content, []byte("\n\n")) {
			separator = nil
		}
		updated := append(bytes.Clone(content), separator...)
		updated = append(updated, table...)
		return replaceConfig(path, content, updated)
	}
	updated := make([]byte, 0, len(content)-(end-start)+len(table)+1)
	updated = append(updated, content[:start]...)
	updated = append(updated, table...)
	if end < len(content) && content[end] != '\n' {
		updated = append(updated, '\n')
	}
	updated = append(updated, content[end:]...)
	return replaceConfig(path, content, updated)
}

func removeAutoboardConfig(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Codex configuration: %w", err)
	}
	header := "[mcp_servers." + MCPName + "]"
	tableCount := autoboardTableCount(content)
	if tableCount > 1 {
		return errors.New("codex configuration has duplicate Autoboard MCP tables")
	}
	start, end, err := tableRange(content, header)
	if err != nil {
		return err
	}
	if tableCount == 1 && start == -1 {
		return errors.New("refuse to modify Autoboard MCP table using alternate TOML syntax")
	}
	if start == -1 {
		return nil
	}
	removeStart := start
	if removeStart > 0 &&
		content[removeStart-1] == '\n' &&
		(removeStart == 1 || content[removeStart-2] == '\n') {
		removeStart--
	}
	updated := make([]byte, 0, len(content)-(end-removeStart))
	updated = append(updated, content[:removeStart]...)
	updated = append(updated, content[end:]...)
	return replaceConfig(path, content, updated)
}

func tableRange(content []byte, header string) (int, int, error) {
	start := -1
	end := len(content)
	offset := 0
	for offset <= len(content) {
		next := bytes.IndexByte(content[offset:], '\n')
		lineEnd := len(content)
		if next >= 0 {
			lineEnd = offset + next + 1
		}
		line := strings.TrimSpace(string(content[offset:lineEnd]))
		if line == header {
			if start != -1 {
				return 0, 0, fmt.Errorf(
					"codex configuration has duplicate %s tables",
					header,
				)
			}
			start = offset
		} else if start != -1 && strings.HasPrefix(line, "[") {
			end = offset
			break
		}
		if next < 0 {
			break
		}
		offset = lineEnd
	}
	return start, end, nil
}

func autoboardTableCount(content []byte) int {
	count := 0
	for line := range bytes.SplitSeq(content, []byte("\n")) {
		keys, ok := tableHeaderKeys(strings.TrimSpace(string(line)))
		if ok &&
			len(keys) == 2 &&
			keys[0] == "mcp_servers" &&
			keys[1] == MCPName {
			count++
		}
	}
	return count
}

func tableHeaderKeys(line string) ([]string, bool) {
	if !strings.HasPrefix(line, "[") || strings.HasPrefix(line, "[[") {
		return nil, false
	}
	quote := byte(0)
	escaped := false
	closeIndex := -1
	for index := 1; index < len(line); index++ {
		character := line[index]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '"' || character == '\'' {
			quote = character
		} else if character == ']' {
			closeIndex = index
			break
		}
	}
	if closeIndex == -1 || quote != 0 {
		return nil, false
	}
	trailing := strings.TrimSpace(line[closeIndex+1:])
	if trailing != "" && !strings.HasPrefix(trailing, "#") {
		return nil, false
	}
	return dottedKeyParts(line[1:closeIndex])
}

func dottedKeyParts(value string) ([]string, bool) {
	var rawParts []string
	quote := byte(0)
	escaped := false
	partStart := 0
	for index := range len(value) {
		character := value[index]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '"', '\'':
			quote = character
		case '.':
			rawParts = append(rawParts, value[partStart:index])
			partStart = index + 1
		}
	}
	if quote != 0 {
		return nil, false
	}
	rawParts = append(rawParts, value[partStart:])
	parts := make([]string, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part, ok := decodeKeyPart(strings.TrimSpace(rawPart))
		if !ok {
			return nil, false
		}
		parts = append(parts, part)
	}
	return parts, true
}

func decodeKeyPart(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	if value[0] == '"' {
		decoded, err := strconv.Unquote(value)
		return decoded, err == nil
	}
	if value[0] == '\'' {
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", false
		}
		return value[1 : len(value)-1], true
	}
	return value, true
}

func replaceConfig(path string, current []byte, updated []byte) error {
	if bytes.Equal(updated, current) {
		return nil
	}
	info, err := os.Stat(path)
	mode := os.FileMode(0o600)
	if err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat Codex configuration: %w", err)
	}
	if err := writeAtomic(path, updated, mode); err != nil {
		return fmt.Errorf("update Autoboard Codex configuration: %w", err)
	}
	return nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".autoboard-config-*")
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
	return os.Rename(temporaryPath, path)
}
