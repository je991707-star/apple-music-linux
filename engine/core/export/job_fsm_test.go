package export

import (
	"context"
	"errors"
	"math/rand"
	"testing"
)

var allPhases = []Phase{PhaseQueued, PhaseResolving, PhaseDownloading, PhaseTagging, PhaseMoving, PhaseDone, PhaseFailed, PhaseCancelled}

func TestCanTransitionTable(t *testing.T) {
	for _, from := range allPhases {
		for _, to := range allPhases {
			want := false
			switch {
			case from.IsTerminal():
				want = false // terminal states are absorbing
			case to.IsTerminal():
				want = true // anything can end
			default:
				want = to.rank() >= from.rank() // forward or in place only
			}
			if got := canTransition(from, to); got != want {
				t.Errorf("canTransition(%s → %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	for _, bogus := range []Phase{"bogus", ""} {
		if canTransition(PhaseQueued, bogus) || canTransition(bogus, PhaseDownloading) || canTransition(bogus, PhaseDone) {
			t.Errorf("unknown phase %q must be refused in both directions", bogus)
		}
	}
}

func TestTerminalStatesAreAbsorbing(t *testing.T) {
	m := NewManager(nil, nil, Options{})
	for _, final := range []Phase{PhaseDone, PhaseFailed, PhaseCancelled} {
		job := &ExportJob{ID: "j", Phase: PhaseDownloading}
		switch final {
		case PhaseDone:
			m.advance(job, PhaseDone, 100)
		case PhaseFailed:
			m.fail(job, errors.New("boom"))
		case PhaseCancelled:
			ctx, cancel := context.WithCancel(context.Background())
			job.ctx = ctx
			cancel()
			m.fail(job, errors.New("cancelled"))
		}
		if job.Phase != final {
			t.Fatalf("setup: reached %s, want %s", job.Phase, final)
		}
		m.advance(job, PhaseDownloading, 10)
		m.advance(job, PhaseDone, 100)
		m.fail(job, errors.New("late error"))
		if job.Phase != final {
			t.Errorf("%s was left: now %s", final, job.Phase)
		}
	}
}

// Whatever order racing goroutines call advance/fail in, the phase sequence a job passes through must be
// a path in the transition relation, and a terminal state, once reached, must be the last one.
func TestRandomEventSequencesOnlyFollowTheRelation(t *testing.T) {
	m := NewManager(nil, nil, Options{})
	rng := rand.New(rand.NewSource(1))
	for run := 0; run < 2000; run++ {
		job := &ExportJob{ID: "j", Phase: PhaseQueued}
		prev := job.Phase
		for step := 0; step < 12; step++ {
			if rng.Intn(5) == 0 {
				m.fail(job, errors.New("x"))
			} else {
				m.advance(job, allPhases[rng.Intn(len(allPhases))], rng.Intn(101))
			}
			if job.Phase != prev && !canTransition(prev, job.Phase) {
				t.Fatalf("run %d: illegal move %s → %s", run, prev, job.Phase)
			}
			if prev.IsTerminal() && job.Phase != prev {
				t.Fatalf("run %d: left terminal state %s for %s", run, prev, job.Phase)
			}
			prev = job.Phase
		}
	}
}
