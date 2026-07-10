package main

import "testing"

func TestPendingEmitFiresOnceBeforeCallback(t *testing.T) {
	na := newNodeArenas()
	calls := 0

	var pending *PendingEmit

	pending = na.pendingEmit(func() {
		calls++
		pending.fire()
	})

	pending.fire()
	pending.fire()

	if calls != 1 {
		t.Fatalf("pending callback calls = %d, want 1", calls)
	}
}
