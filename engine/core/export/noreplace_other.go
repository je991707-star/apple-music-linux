//go:build !linux

package export

// renameNoReplace has no atomic form here; the caller claims the name and copies.
func renameNoReplace(_, _ string) error { return errNoReplaceUnsupported }
