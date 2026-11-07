//go:build !linux

package main

import "fmt"

func restrictScopedFiles() error {
	return fmt.Errorf("private Main workspace requires Linux")
}
