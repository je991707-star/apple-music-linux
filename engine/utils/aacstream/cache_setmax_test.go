package aacstream

import "testing"

func TestSetMaxBytesClampsNegative(t *testing.T) {
	old := MVCacheMaxBytes()
	defer SetMVCacheMaxBytes(old)
	SetMVCacheMaxBytes(-5)
	if MVCacheMaxBytes() != 0 || MVCacheEnabled() {
		t.Fatalf("negative max must disable the MV cache, got %d", MVCacheMaxBytes())
	}
	SetMVCacheMaxBytes(1 << 20)
	if !MVCacheEnabled() {
		t.Fatal("positive max must enable it")
	}
}
