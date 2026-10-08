package playback

import (
	"context"
	"testing"
	"time"

	"engine/core/media"
)

type panicOnceProvider struct {
	stubProvider
	panicked bool
}

func (p *panicOnceProvider) Open(ctx context.Context, req media.OpenRequest) (*media.Session, error) {
	if !p.panicked {
		p.panicked = true
		panic("malformed manifest")
	}
	return p.stubProvider.Open(ctx, req)
}

// A provider panic inside the singleflight leader must not leave the in-flight entry
// behind: every later Open for that asset would otherwise join a flight that never ends.
func TestOpen_LeaderPanicDoesNotWedgeAsset(t *testing.T) {
	m := newTestManager(&panicOnceProvider{})
	req := OpenRequest{AssetID: "123", Storefront: "us"}

	if sess, err := m.Open(context.Background(), req); sess != nil || err == nil {
		t.Fatalf("a provider panic must surface as an error, got sess=%v err=%v", sess, err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := m.Open(context.Background(), req)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second Open failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Open hung: the failed flight was never cleaned up")
	}
}
