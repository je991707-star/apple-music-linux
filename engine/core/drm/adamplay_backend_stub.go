//go:build !adamplay_backend || !(linux || windows)

package drm

// AdamplayBackend returns nil when the adamplay_backend build tag is absent
// (i.e. the binary was built without CGO DRM support). This stub prevents
// apiserver.go from failing to compile when the tag is not set.
func AdamplayBackend(drmDir string) DRMBackend {
	return nil
}
