package drm

import "sync"

// devTokenCache holds the MusicKit developer JWT used for license requests. It is fetched when the
// backend starts, but that single request can fail (a DNS timeout, a flaky network), and the license
// requests that follow would then go out with no token until the engine restarted. get retries at
// the point of use instead.
type devTokenCache struct {
	mu  sync.Mutex
	tok string
}

func (c *devTokenCache) set(t string) {
	c.mu.Lock()
	c.tok = t
	c.mu.Unlock()
}

// get returns the cached token, else fromAccount (a token the account source already carries), else
// asks fetch for one and caches it. It returns "" if none can be had. fetch runs without the lock held.
func (c *devTokenCache) get(fromAccount string, fetch func() (string, error)) string {
	c.mu.Lock()
	t := c.tok
	c.mu.Unlock()
	if t != "" {
		return t
	}
	if fromAccount != "" {
		return fromAccount
	}
	if fetch == nil {
		return ""
	}
	t, err := fetch()
	if err != nil || t == "" {
		return ""
	}
	c.set(t)
	return t
}
