package tcp

import "sync"

type Vclock struct {
	mu    sync.RWMutex
	clock map[string]uint64
}

func NewVclock() *Vclock {
	return &Vclock{clock: make(map[string]uint64)}
}

func FromMap(m map[string]uint64) *Vclock {
	vc := NewVclock()
	for k, v := range m {
		vc.clock[k] = v
	}

	return vc
}

func (vc *Vclock) Tick(nodeID string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.clock[nodeID]++
}

func (vc *Vclock) Merge(other map[string]uint64) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	for k, v := range other {
		if vc.clock[k] < v {
			vc.clock[k] = v
		}
	}
}

func (vc *Vclock) Snapshot() map[string]uint64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	snap := make(map[string]uint64, len(vc.clock))
	for k, v := range vc.clock {
		snap[k] = v
	}

	return snap
}

type Relation int

const (
	Before     Relation = -1
	Concurrent Relation = 0
	After      Relation = 1
)

func Compare(a, b map[string]uint64) Relation {
	aBeforeB := true
	bBeforeA := true

	keys := make(map[string]struct{})
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}

	for k := range keys {
		av, bv := a[k], b[k]
		if av > bv {
			bBeforeA = false
		}
		if av < bv {
			aBeforeB = false
		}
	}

	switch {
	case aBeforeB && !bBeforeA:
		return Before
	case bBeforeA && !aBeforeB:
		return After
	case aBeforeB && bBeforeA:
		return Before
	default:
		return Concurrent
	}
}
