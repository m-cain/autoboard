package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	os.Exit(run("web/dist", "internal/webui/dist", os.Stderr))
}

func run(source string, target string, stderr io.Writer) int {
	if err := prepare(source, target); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func prepare(source string, target string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil || !sourceInfo.IsDir() {
		return fmt.Errorf("browser build is missing at %s", source)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create embedded asset directory: %w", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return fmt.Errorf("read embedded asset directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == ".placeholder" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(target, entry.Name())); err != nil {
			return fmt.Errorf("remove old embedded asset %s: %w", entry.Name(), err)
		}
	}
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		return fmt.Errorf("copy browser build for embedding: %w", err)
	}
	return nil
}
