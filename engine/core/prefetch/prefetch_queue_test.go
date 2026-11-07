package prefetch

import "testing"

func TestWorkQueuePushAfterCloseReturnsItem(t *testing.T) {
	q := newWorkQueue()
	q.close()
	it := &workItem{baseScore: 1}
	if got := q.push(it); got != it {
		t.Fatal("push on a closed queue must hand the item back")
	}
	if q.depth() != 0 {
		t.Fatal("closed queue must not accept work")
	}
}

func TestWorkQueueEvictsLowestScore(t *testing.T) {
	q := newWorkQueue()
	for i := 0; i < maxQueueDepth; i++ {
		if ev := q.push(&workItem{baseScore: float64(i + 1)}); ev != nil {
			t.Fatalf("unexpected eviction while filling: %v", ev.baseScore)
		}
	}
	low := &workItem{baseScore: 0.5}
	if got := q.push(low); got != low {
		t.Fatal("a lower-scored newcomer must be the one rejected")
	}
	if got := q.push(&workItem{baseScore: 1e9}); got == nil || got.baseScore != 1 {
		t.Fatalf("expected the lowest queued item (1) evicted, got %+v", got)
	}
	top, ok := q.pop()
	if !ok || top.baseScore != 1e9 {
		t.Fatalf("pop must return the highest score first, got %+v", top)
	}
}
