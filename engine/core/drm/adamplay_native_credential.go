//go:build (linux || windows) && adamplay_backend

package drm

import "sync"

func (b *adamplayBackend) nativeCredentialConsumer() (privateInputConsumer, func()) {
	endpoint := newNativeCredentialEndpoint()
	if endpoint == nil || !endpoint.available() {
		return nil, nil
	}
	adapter := newNativeCredentialAdapter(&fencedCredentialEndpoint{nativeCredentialEndpoint: endpoint, backend: b})
	return adapter.consumer()
}

type fencedCredentialEndpoint struct {
	nativeCredentialEndpoint
	backend    *adamplayBackend
	mu         sync.Mutex
	generation uint64
}

func (e *fencedCredentialEndpoint) submit(tag [32]byte, g uint64, kind, id string, in *PrivateInput) (bool, error) {
	e.mu.Lock()
	if e.generation == 0 {
		e.generation = e.backend.currentGen()
	}
	gen := e.generation
	e.mu.Unlock()
	if gen == 0 {
		return false, errBackendStopped
	}
	leave, err := e.backend.enter(gen)
	if err != nil {
		return false, err
	}
	defer leave()
	return e.nativeCredentialEndpoint.submit(tag, g, kind, id, in)
}

func (b *adamplayBackend) nativeCredentialShutdown() func() { return shutdownNativeCredentialProcess }
func (e *fencedCredentialEndpoint) shutdown() {
	if inner, ok := e.nativeCredentialEndpoint.(interface{ shutdown() }); ok {
		inner.shutdown()
	} else {
		e.nativeCredentialEndpoint.cancel()
	}
}
