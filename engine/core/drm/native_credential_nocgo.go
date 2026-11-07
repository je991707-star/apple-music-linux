//go:build !linux || !cgo

package drm

func newNativeCredentialEndpoint() nativeCredentialEndpoint { return nil }

func shutdownNativeCredentialProcess() {}
