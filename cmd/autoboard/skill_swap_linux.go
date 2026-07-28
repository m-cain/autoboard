//go:build linux

package main

import "golang.org/x/sys/unix"

func atomicSwapDirectories(first string, second string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		first,
		unix.AT_FDCWD,
		second,
		unix.RENAME_EXCHANGE,
	)
}
