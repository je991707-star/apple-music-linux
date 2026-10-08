package drm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sessionValidityTTL = 4 * time.Hour

// SessionManager is the engine's authoritative source for DRM session state.
//
// Ownership: SessionManager owns everything related to persisted session state.
//
//	mpl_db/ directory and all SQLite databases inside it
//	STOREFRONT_ID file
//	MUSIC_TOKEN file
//	drm-state file (path only; written by wrapper, read by SessionManager)
//	delete (ClearSession removes all of the above)
//	behavioral validity tracking (last successful DRM op timestamp)
//
// SessionManager does NOT own:
//
//	live authenticated state — that's AdamplayBackend (drm_init state)
//	Apple credentials — that's AuthCoordinator
//
// Nothing outside this type reads or deletes the files it owns.
// AdamplayBackend writes drm-state; SessionManager provides the path.
//
// Session validity is behavioral, not purely filesystem-based. A session is
// considered Valid only when:
//  1. mpl_db/accounts.sqlitedb exists and is non-empty, AND
//  2. STOREFRONT_ID and MUSIC_TOKEN are present, AND
//  3. A successful DRM operation has been observed within sessionValidityTTL
//     OR a live GetAccount() call succeeds.
//
// This design prevents the engine from claiming a session is valid solely
// because the files exist from a past, now-expired login.
type SessionManager struct {
	baseDir string

	// webSession supplies the media-user-token (and storefront, when known) the web player handed
	// to the engine. It stands in for the credential files an old login wrote, so a profile that
	// only ever signed in on the web still counts as having a session.
	webSession func() (mut, storefront string)

	mu          sync.Mutex
	lastSuccess time.Time
}

// NewSessionManager creates a SessionManager rooted at baseDir.
func NewSessionManager(baseDir string) *SessionManager {
	return &SessionManager{baseDir: baseDir}
}

// SetWebSession registers where the web player's media-user-token comes from. The function must be
// cheap and must not call back into the SessionManager.
func (s *SessionManager) SetWebSession(fn func() (mut, storefront string)) {
	s.mu.Lock()
	s.webSession = fn
	s.mu.Unlock()
}

func (s *SessionManager) web() (mut, storefront string) {
	s.mu.Lock()
	fn := s.webSession
	s.mu.Unlock()
	if fn == nil {
		return "", ""
	}
	return fn()
}

// HasSession reports whether an account is available: either the legacy login database
// (mpl_db/accounts.sqlitedb) exists and has content, or the web player has supplied a
// media-user-token. It does NOT guarantee the session is currently usable — use IsSessionValid.
func (s *SessionManager) HasSession() bool {
	path := filepath.Join(s.baseDir, "mpl_db", "accounts.sqlitedb")
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return true
	}
	mut, _ := s.web()
	return mut != ""
}

// RecordSuccess records the timestamp of a successful DRM operation.
// DRMManager calls this after every successful Decrypt, GetM3U8, or
// GetAccount response. This is the behavioral signal that keeps
// SessionState == SessionValid.
func (s *SessionManager) RecordSuccess() {
	s.mu.Lock()
	s.lastSuccess = time.Now()
	s.mu.Unlock()
}

// IsSessionValid returns (true, nil) when the session appears usable.
// It first checks filesystem prerequisites, then validates behaviorally:
//   - If a successful DRM operation was observed within sessionValidityTTL, returns true.
//   - Otherwise attempts a live GetAccount() call to confirm the session works.
//
// The drm parameter is the DRMProvider to use for the live check; pass nil
// to skip the live check and rely on the TTL only.
func (s *SessionManager) IsSessionValid(ctx context.Context, drm DRMProvider) (bool, error) {
	if !s.HasSession() {
		return false, nil
	}
	if s.ReadStorefrontID() == "" || s.ReadMusicToken() == "" {
		return false, nil
	}

	s.mu.Lock()
	last := s.lastSuccess
	s.mu.Unlock()

	if !last.IsZero() && time.Since(last) < sessionValidityTTL {
		return true, nil
	}

	// No recent success on record — try a live confirmation.
	if drm == nil {
		// Caller opted out of live check; treat as expired.
		return false, nil
	}
	_, err := drm.GetAccount(ctx)
	if err == nil {
		s.RecordSuccess()
		return true, nil
	}
	return false, nil
}

// SessionInfo returns the cached storefront ID and music token.
// Returns nil if either file is missing or empty.
func (s *SessionManager) SessionInfo() *SessionInfo {
	sf := s.ReadStorefrontID()
	mt := s.ReadMusicToken()
	if sf == "" || mt == "" {
		return nil
	}
	return &SessionInfo{StorefrontID: sf, MusicToken: mt}
}

// SessionInfo holds cached account identifiers.
type SessionInfo struct {
	StorefrontID string
	MusicToken   string
}

// ClearSession removes mpl_db/, STOREFRONT_ID, MUSIC_TOKEN, and drm-state.
// The backend process must be stopped before calling ClearSession to avoid
// storeservicescore writing the files back during deletion.
func (s *SessionManager) ClearSession() error {
	paths := []string{
		filepath.Join(s.baseDir, "mpl_db"),
		filepath.Join(s.baseDir, "STOREFRONT_ID"),
		filepath.Join(s.baseDir, "MUSIC_TOKEN"),
		filepath.Join(s.baseDir, "drm-state"),
	}
	var lastErr error
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
			lastErr = err
		}
	}
	s.mu.Lock()
	s.lastSuccess = time.Time{}
	s.mu.Unlock()
	return lastErr
}

// StateFilePath returns the path of the drm-state file written by main.c.
func (s *SessionManager) StateFilePath() string {
	return filepath.Join(s.baseDir, "drm-state")
}

// TwoFAFilePath returns the path of the 2fa.txt file used by the
// --code-from-file mechanism in the wrapper's credentialHandler.
func (s *SessionManager) TwoFAFilePath() string {
	return filepath.Join(s.baseDir, "2fa.txt")
}

// ReadStorefrontID reads {BaseDir}/STOREFRONT_ID.
func (s *SessionManager) ReadStorefrontID() string {
	if sf := readFile(filepath.Join(s.baseDir, "STOREFRONT_ID")); sf != "" {
		return sf
	}
	_, sf := s.web()
	return sf
}

// NormalizeStorefrontID strips the platform/content-class suffix that the
// wrapper appends to the raw STOREFRONT_ID file. The wrapper writes values
// like "143467-2,31" (numeric ID + content-class suffix); the MusicKit
// catalog API expects only the bare numeric ID ("143467").
//
// The format observed at runtime is "<id>-<platform>,<class>". We split on
// the first "-" and discard everything after it. If the value contains no
// "-", it is returned unchanged (already normalized, or an ISO 2-letter code
// like "us" set via config.yaml).
func NormalizeStorefrontID(sf string) string {
	if i := strings.IndexByte(sf, '-'); i >= 0 {
		return sf[:i]
	}
	return sf
}

// ReadMusicToken reads the current MUSIC_TOKEN from disk.
//
// It intentionally performs a fresh read on every call. The wrapper may
// refresh the token during the engine's lifetime, so callers must not
// assume the value is immutable or cache it.
func (s *SessionManager) ReadMusicToken() string {
	if t := readFile(filepath.Join(s.baseDir, "MUSIC_TOKEN")); t != "" {
		return t
	}
	t, _ := s.web()
	return t
}

// readFile returns the file's content without surrounding whitespace: these
// values go into HTTP headers, where a trailing newline is invalid.
func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
