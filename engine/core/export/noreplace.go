package export

import (
	"errors"
	"io"
	"os"
)

// moveNoReplace moves src to dst only if dst does not exist, and never replaces a
// file that appears at dst meanwhile. It returns an error wrapping fs.ErrExist when
// dst is taken. Linux tries an atomic rename first (renameNoReplace); otherwise the
// name is claimed with O_EXCL and src is copied into the claimed file, so an
// os.Rename (which would replace a racing file) is never used.
func moveNoReplace(src, dst string) error {
	if err := renameNoReplace(src, dst); !errors.Is(err, errNoReplaceUnsupported) {
		return err
	}
	return copyIntoNew(src, dst)
}

var errNoReplaceUnsupported = errors.New("no-replace rename unsupported")

// copyIntoNew claims dst exclusively, copies src into it and removes src. A failed
// copy removes the claimed (partial) dst, which is ours: nobody else can have
// written it.
func copyIntoNew(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err // fs.ErrExist when the name is taken
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst) //nolint:errcheck
		return err
	}
	os.Remove(src) //nolint:errcheck
	return nil
}
