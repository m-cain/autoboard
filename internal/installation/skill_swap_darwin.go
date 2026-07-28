//go:build darwin

package installation

import "golang.org/x/sys/unix"

// atomicSwapSkillDirectories exchanges the staged and installed skills without
// leaving the Codex skill path absent during a refresh.
func atomicSwapSkillDirectories(first string, second string) error {
	return unix.RenameatxNp(
		unix.AT_FDCWD,
		first,
		unix.AT_FDCWD,
		second,
		unix.RENAME_SWAP,
	)
}
