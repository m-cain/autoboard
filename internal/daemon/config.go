package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultHTTPPort = 4040
	defaultDataName = "Autoboard"
)

type Config struct {
	Address            string
	DatabasePath       string
	DataDir            string
	MaxAttachmentBytes int64
	Development        bool
}

func ConfigFromEnvironment() (Config, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return loadConfig(userConfigDir, os.Getenv)
}

func loadConfig(
	userConfigDir string,
	getenv func(string) string,
) (Config, error) {
	port := defaultHTTPPort
	if raw := getenv("AUTOBOARD_HTTP_PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65_535 {
			return Config{}, errors.New("AUTOBOARD_HTTP_PORT must be an integer from 1 through 65535")
		}
		port = parsed
	}
	dataDir := getenv("AUTOBOARD_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(userConfigDir, defaultDataName)
	}
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve Autoboard data directory: %w", err)
	}
	databasePath := getenv("AUTOBOARD_DATABASE_PATH")
	if databasePath == "" {
		databasePath = filepath.Join(dataDir, "autoboard.db")
	}
	databasePath, err = filepath.Abs(databasePath)
	if err != nil {
		return Config{}, fmt.Errorf("resolve Autoboard database path: %w", err)
	}
	relativeDatabasePath, err := filepath.Rel(dataDir, databasePath)
	if err != nil ||
		relativeDatabasePath == ".." ||
		filepath.IsAbs(relativeDatabasePath) ||
		strings.HasPrefix(relativeDatabasePath, ".."+string(filepath.Separator)) {
		return Config{}, errors.New("AUTOBOARD_DATABASE_PATH must be inside AUTOBOARD_DATA_DIR")
	}
	var maxAttachmentBytes int64
	if raw := getenv("AUTOBOARD_MAX_ATTACHMENT_BYTES"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			return Config{}, errors.New("AUTOBOARD_MAX_ATTACHMENT_BYTES must be a positive integer")
		}
		maxAttachmentBytes = parsed
	}
	return Config{
		Address:            fmt.Sprintf("127.0.0.1:%d", port),
		DatabasePath:       databasePath,
		DataDir:            dataDir,
		MaxAttachmentBytes: maxAttachmentBytes,
		Development:        getenv("AUTOBOARD_DEVELOPMENT") == "1",
	}, nil
}
