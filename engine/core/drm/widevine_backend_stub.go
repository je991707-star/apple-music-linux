//go:build !widevine_backend || !(linux || windows)

package drm

// WidevineBackend returns nil when the widevine_backend build tag is absent
// (i.e. the binary was built without CGO DRM support). This stub prevents
// apiserver.go from failing to compile when the tag is not set.
func WidevineBackend(drmDir string) DRMBackend {
	return nil
}
