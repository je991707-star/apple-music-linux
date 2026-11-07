package drm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// logoutScope is how much a sign-out removes. A sign-out (account) removes the
// Apple account and keeps the device identity, as Apple's own Android and Windows
// apps do; "Forget This Device" (device) removes the device identity too, so Apple
// treats the next sign-in as a new device and asks for a verification code.
type logoutScope int

const (
	scopeNone logoutScope = iota
	scopeAccount
	scopeDevice
)

func (s logoutScope) String() string {
	switch s {
	case scopeAccount:
		return "account"
	case scopeDevice:
		return "device"
	}
	return "none"
}

// parseScope reads a marker's contents. A marker exists, so it asks for at least
// an account sign-out: anything other than "device" (the old code's "1", empty,
// unreadable) is an account sign-out, so recovery never removes device files
// nobody asked to remove.
func parseScope(b []byte) logoutScope {
	if strings.TrimSpace(string(b)) == "device" {
		return scopeDevice
	}
	return scopeAccount
}

// mergeScope keeps the stronger request: device wins over account.
func mergeScope(a, b logoutScope) logoutScope {
	if a > b {
		return a
	}
	return b
}

const (
	logoutMarkerName = "logout-pending"
	backstopPrefix   = "logout-pending.backstop-"
)

// backstopRe matches the files the Electron main process writes when it could not
// reach the engine during a sign-out: one uniquely named file per request, never
// replaced. Their .tmp files are uncommitted writes and are not requests.
var (
	backstopRe    = regexp.MustCompile(`^logout-pending\.backstop-[0-9a-f]{32}$`)
	backstopTmpRe = regexp.MustCompile(`^logout-pending\.backstop-[0-9a-f]{32}\.tmp$`)
)

// staleTmpAge is how old an uncommitted backstop .tmp must be before a completed
// cleanup removes it: a younger one may belong to a write still in progress.
const staleTmpAge = 5 * time.Minute

// pendingLogout is every sign-out request found on disk: the engine's own marker
// and the main process's backstops, merged into one scope.
type pendingLogout struct {
	scope logoutScope
	files []string // full paths, exactly the files this pass consumes
}

func (p pendingLogout) any() bool { return len(p.files) > 0 }

// pending lists the sign-out requests on disk. It reads the directory each time,
// so a request written by another process is seen by the next caller.
func (s *SessionManager) pending() (pendingLogout, error) {
	var p pendingLogout
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return p, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (name != logoutMarkerName && !backstopRe.MatchString(name)) {
			continue
		}
		path := filepath.Join(s.baseDir, name)
		b, _ := os.ReadFile(path) // unreadable: still a request, at least an account sign-out
		p.scope = mergeScope(p.scope, parseScope(b))
		p.files = append(p.files, path)
	}
	return p, nil
}

// hasPending reports whether any sign-out request is on disk. A read error counts
// as pending: no session may be used while that is unknown.
func (s *SessionManager) hasPending() bool {
	s.mu.Lock()
	unsynced := s.unsynced
	s.mu.Unlock()
	if unsynced {
		return true
	}
	p, err := s.pending()
	return err != nil || p.any()
}

// MarkLogoutPending durably records a sign-out request before anything is stopped
// or deleted, merged with an engine marker that is already there (device wins). An
// error means nothing was recorded, and the caller must not start the sign-out.
func (s *SessionManager) MarkLogoutPending(scope logoutScope) error {
	if err := os.MkdirAll(s.baseDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(s.baseDir, logoutMarkerName)
	if b, err := os.ReadFile(path); err == nil {
		scope = mergeScope(scope, parseScope(b))
	}
	return s.hook().writeMarker(path, []byte(scope.String()))
}

// accountFiles are the Apple account: the login database, tokens, the session
// marker and FairPlay's per-account keybags. deviceFiles are the device identity
// Apple recognises the machine by; only "Forget This Device" removes them.
func (s *SessionManager) accountFiles() []string {
	paths := []string{
		filepath.Join(s.baseDir, "mpl_db"),
		filepath.Join(s.baseDir, "STOREFRONT_ID"),
		filepath.Join(s.baseDir, "MUSIC_TOKEN"),
		filepath.Join(s.baseDir, "drm-state"),
		filepath.Join(s.baseDir, "session-ready"),
		filepath.Join(s.baseDir, "2fa.txt"),
	}
	return append(paths, fairPlayKeyFiles(s.baseDir)...)
}

func (s *SessionManager) deviceFiles() []string {
	return []string{
		filepath.Join(s.baseDir, deviceIDFile),
		filepath.Join(s.baseDir, "adi.pb"),
		filepath.Join(s.baseDir, "IC-Info.sids"),
		filepath.Join(s.baseDir, "fsi.pdat"),
	}
}

// deleteScope removes the files of a scope. Every file is tried; the first error
// is returned. Deleting is idempotent, so a repeated pass is harmless.
func (s *SessionManager) deleteScope(scope logoutScope) error {
	paths := s.accountFiles()
	if scope == scopeDevice {
		paths = append(paths, s.deviceFiles()...)
	}
	var first error
	for _, p := range paths {
		if err := s.hook().removeAll(p); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	s.mu.Lock()
	s.lastSuccess = time.Time{}
	s.mu.Unlock()
	return first
}

// finishPending removes exactly the request files a completed pass consumed, by
// name, and then syncs the directory: only after that may a new session start, or
// a removed but not yet durable request could reappear after a crash and wipe it.
func (s *SessionManager) finishPending(files []string) error {
	h := s.hook()
	for _, f := range files {
		if err := h.removeAll(f); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return s.syncRemovals()
}

// syncRemovals makes the removals durable. Until it succeeds the sign-out counts as
// pending (unsynced), even though the files are already gone from the directory:
// they could come back after a crash.
func (s *SessionManager) syncRemovals() error {
	if err := s.hook().syncDir(s.baseDir); err != nil {
		s.mu.Lock()
		s.unsynced = true
		s.mu.Unlock()
		return fmt.Errorf("sync %s: %w", s.baseDir, err)
	}
	s.mu.Lock()
	s.unsynced = false
	s.mu.Unlock()
	s.removeStaleTmp()
	return nil
}

// removeStaleTmp drops uncommitted backstop writes old enough to be abandoned.
func (s *SessionManager) removeStaleTmp() {
	entries, _ := os.ReadDir(s.baseDir)
	for _, e := range entries {
		if !backstopTmpRe.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTmpAge {
			_ = os.Remove(filepath.Join(s.baseDir, e.Name()))
		}
	}
}

// runPendingPass completes every sign-out request on disk: delete the merged
// scope, remove the consumed request files, sync. A request written while a pass
// runs has a new name and is picked up by the next round.
func (s *SessionManager) runPendingPass() error {
	for round := 0; round < 8; round++ {
		p, err := s.pending()
		if err != nil {
			return err
		}
		if !p.any() {
			s.mu.Lock()
			unsynced := s.unsynced
			s.mu.Unlock()
			if unsynced {
				return s.syncRemovals()
			}
			return nil
		}
		if err := s.deleteScope(p.scope); err != nil {
			return err
		}
		if err := s.finishPending(p.files); err != nil {
			return err
		}
	}
	return errors.New("sign-out requests keep arriving")
}

// fileHooks are the filesystem operations sign-out depends on, replaceable in tests
// to force failures or block deterministically.
type fileHooks struct {
	writeMarker func(path string, data []byte) error
	removeAll   func(path string) error
	syncDir     func(dir string) error
}

var defaultFileHooks = fileHooks{
	writeMarker: writeDurable,
	removeAll:   os.RemoveAll,
	syncDir:     syncDirectory,
}

func (s *SessionManager) hook() fileHooks {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hooks == nil {
		return defaultFileHooks
	}
	return *s.hooks
}

// writeDurable writes data to path so it survives a crash: a temp file, fsync,
// rename, then a directory fsync. Nothing is visible at path until the rename.
func writeDurable(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// syncDirectory makes renames and removals in dir durable. Windows cannot fsync a
// directory from Go, so there directory-entry durability is not guaranteed (Adam
// has no DRM sign-in on Windows).
func syncDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
