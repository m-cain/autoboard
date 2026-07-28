//go:build !darwin && !linux

package main

import "errors"

func atomicSwapDirectories(string, string) error {
	return errors.New("atomic skill directory exchange requires macOS or Linux")
}
