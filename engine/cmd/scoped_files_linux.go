//go:build linux

package main

import "syscall"

// Called once at private Engine startup, before caches or Native libraries can
// create files. This changes only the owned Engine's future file permissions.
func restrictScopedFiles() error {
	syscall.Umask(0o077)
	return nil
}
