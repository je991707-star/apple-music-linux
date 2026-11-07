package aacstream

// mvcache_dec.go — the decrypted-MV disk cache (~/.cache/adam/engine/mv-dec/*.enc).
// Nothing writes it any more; its size is still reported and Clear still removes
// what earlier versions left on disk.

import (
	"engine/internal/appdir"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	mvDecDirName = "engine/mv-dec"
)

var (
	mvDecDir     string
	mvDecTotalSz atomic.Int64

	mvDecMu sync.Mutex
)

var mvDecOnce sync.Once

// mvDecDirPath returns the encrypted MV cache directory. It is created, and the size of
// what is already in it counted, on first use rather than at import.
func mvDecDirPath() string {
	mvDecOnce.Do(func() {
		if mvDecDir == "" {
			mvDecDir = filepath.Join(appdir.Cache(), mvDecDirName)
			// Earlier versions kept this cache directly under the OS cache root.
			if old, err := os.UserCacheDir(); err == nil {
				appdir.MoveInto(filepath.Join(old, mvDecDirName), mvDecDir)
			}
		}
		os.MkdirAll(mvDecDir, 0700)

		// Account for existing cached files. The directory holds at most ~10-20 large
		// files (bounded by the 2 GB segment cache limit), so the walk is fast.
		var total int64
		filepath.Walk(mvDecDir, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".enc") {
				total += info.Size()
			}
			return nil
		})
		mvDecTotalSz.Store(total)
	})
	return mvDecDir
}

// MVDecTotalBytes returns the total on-disk size of all cached encrypted track files.
func MVDecTotalBytes() int64 {
	mvDecDirPath() // count what is already on disk before the first read
	return mvDecTotalSz.Load()
}

// ClearMVDecCache deletes all cached encrypted track files.
// Uses an atomic rename so in-progress writers keep writing to the old directory
// rather than hitting ENOENT after RemoveAll.
func ClearMVDecCache() error {
	mvDecMu.Lock()
	defer mvDecMu.Unlock()

	// Rename the old directory aside so in-flight writers (which have open file
	// handles) continue to succeed; their files will be cleaned up below.
	dir := mvDecDirPath()
	old := dir + ".clearing"
	_ = os.RemoveAll(old) // remove any stale .clearing dir from a previous crash
	if err := os.Rename(dir, old); err != nil && !os.IsNotExist(err) {
		return err
	}
	mvDecTotalSz.Store(0)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Remove the old directory in the background so the caller isn't blocked by
	// disk I/O proportional to how much was cached.
	go os.RemoveAll(old)
	return nil
}
