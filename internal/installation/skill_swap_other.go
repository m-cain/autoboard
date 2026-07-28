//go:build !darwin && !linux

package installation

import "errors"

func atomicSwapSkillDirectories(string, string) error {
	return errors.New("atomic skill directory exchange requires macOS or Linux")
}
