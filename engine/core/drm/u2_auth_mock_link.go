//go:build linux && cgo && adamplay_backend && u2_auth_mock

package drm

/*
// This symbol exists only in the offline Lab substitute. The mock tests cannot
// link against the production DRM library, even if its path is supplied by mistake.
extern int u213_mock_sentinel(void);
*/
import "C"

func u213MockLinked() bool { return C.u213_mock_sentinel() == 2131304 }
