package drm

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDevTokenPrefersTheCachedValue(t *testing.T) {
	var c devTokenCache
	c.set("cached")
	calls := 0
	got := c.get("acct", func() (string, error) { calls++; return "fetched", nil })
	if got != "cached" || calls != 0 {
		t.Fatalf("got %q after %d fetches, want the cached token with no fetch", got, calls)
	}
}

func TestDevTokenFallsBackToTheAccountToken(t *testing.T) {
	var c devTokenCache
	if got := c.get("acct", func() (string, error) { t.Fatal("must not fetch when the account has one"); return "", nil }); got != "acct" {
		t.Fatalf("got %q", got)
	}
}

// The start-up fetch failed: the next use must try again, and keep what it gets.
func TestDevTokenRetriesAfterAFailedStartupFetch(t *testing.T) {
	var c devTokenCache
	fail := true
	var calls int
	fetch := func() (string, error) {
		calls++
		if fail {
			return "", errors.New("dns timeout")
		}
		return "fresh", nil
	}
	if got := c.get("", fetch); got != "" {
		t.Fatalf("a failed fetch must yield no token, got %q", got)
	}
	fail = false
	if got := c.get("", fetch); got != "fresh" {
		t.Fatalf("the retry must succeed, got %q", got)
	}
	if got := c.get("", fetch); got != "fresh" || calls != 2 {
		t.Fatalf("a fetched token must be cached: got %q after %d fetches", got, calls)
	}
}

func TestDevTokenIgnoresEmptyFetchResults(t *testing.T) {
	var c devTokenCache
	if got := c.get("", func() (string, error) { return "", nil }); got != "" {
		t.Fatalf("got %q", got)
	}
	if c.get("", nil) != "" {
		t.Fatal("a nil fetcher yields no token")
	}
}

func TestDevTokenIsSafeUnderConcurrency(t *testing.T) {
	var c devTokenCache
	var n atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.get("", func() (string, error) { n.Add(1); return "t", nil }) }()
	}
	wg.Wait()
	if c.get("", nil) != "t" {
		t.Fatal("token must be cached after concurrent use")
	}
}
