package installation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Record struct {
	Checkout         string    `json:"checkout"`
	CheckoutRevision string    `json:"checkout_revision"`
	BinarySHA256     string    `json:"binary_sha256"`
	Version          string    `json:"version"`
	InstalledAt      time.Time `json:"installed_at"`
}

func NewRecord(
	ctx context.Context,
	checkout string,
	executable string,
	version string,
) (Record, error) {
	checkout, err := filepath.Abs(checkout)
	if err != nil {
		return Record{}, fmt.Errorf("resolve checkout: %w", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "go.mod")); err != nil {
		return Record{}, fmt.Errorf("validate checkout: %w", err)
	}
	revision, err := CheckoutRevision(ctx, checkout)
	if err != nil {
		return Record{}, err
	}
	fingerprint, err := FileSHA256(executable)
	if err != nil {
		return Record{}, err
	}
	return Record{
		Checkout:         checkout,
		CheckoutRevision: revision,
		BinarySHA256:     fingerprint,
		Version:          version,
		InstalledAt:      time.Now().UTC(),
	}, nil
}

func CheckoutRevision(ctx context.Context, checkout string) (string, error) {
	//nolint:gosec // The executable and arguments are fixed; checkout is passed as data.
	revisionCommand := exec.CommandContext(
		ctx,
		"git",
		"-C",
		checkout,
		"rev-parse",
		"HEAD",
	)
	revisionCommand.Env = withoutGitRepositoryEnvironment(os.Environ())
	revisionOutput, err := revisionCommand.Output()
	if err != nil {
		return "", fmt.Errorf("read checkout revision: %w", err)
	}
	return strings.TrimSpace(string(revisionOutput)), nil
}

func withoutGitRepositoryEnvironment(environment []string) []string {
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

func WriteRecord(path string, record Record) error {
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode installation record: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := writeAtomic(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write installation record: %w", err)
	}
	return nil
}

func ReadRecord(path string) (Record, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("read installation record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(content, &record); err != nil {
		return Record{}, fmt.Errorf("decode installation record: %w", err)
	}
	if record.Checkout == "" || record.BinarySHA256 == "" {
		return Record{}, errors.New("installation record is incomplete")
	}
	return record, nil
}

func FileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s for fingerprint: %w", path, err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}
