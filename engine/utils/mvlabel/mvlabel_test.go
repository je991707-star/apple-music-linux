package mvlabel

import (
	"sync"
	"testing"
)

func TestSetGetConcurrent(t *testing.T) {
	Set("")
	if Get() != "" {
		t.Fatal("unset label must be empty")
	}
	var wg sync.WaitGroup
	for _, l := range []string{"1920x1080", "1280x720", "3840x2160"} {
		wg.Add(2)
		go func() { defer wg.Done(); Set(l) }()
		go func() { defer wg.Done(); _ = Get() }()
	}
	wg.Wait()
	Set("1920x1080")
	if got := Get(); got != "1920x1080" {
		t.Fatalf("Get() = %q", got)
	}
}
