package main

import "testing"

func TestIdSet_AddHas(t *testing.T) {
	var s IdSet
	s.reset(8)

	if s.has(VFS(3)) {
		t.Fatal("fresh set reports a member")
	}

	s.add(VFS(3))
	s.add(VFS(5))

	if !s.has(VFS(3)) || !s.has(VFS(5)) {
		t.Fatal("added ids not present")
	}

	if s.has(VFS(4)) {
		t.Fatal("never-added id present")
	}
}

func TestIdSet_ResetClearsMembershipReusingArray(t *testing.T) {
	var s IdSet
	s.reset(8)
	s.add(VFS(2))

	if !s.has(VFS(2)) {
		t.Fatal("member missing before reset")
	}

	before := cap(s.gen)
	s.reset(8) // same size: bump epoch, keep the backing array

	if s.has(VFS(2)) {
		t.Fatal("member survived reset")
	}

	if cap(s.gen) != before {
		t.Fatalf("reset reallocated backing array (cap %d -> %d) for an unchanged size", before, cap(s.gen))
	}
}

func TestIdSet_AddGrowsBeyondLen(t *testing.T) {
	var s IdSet
	s.reset(4)
	s.add(VFS(100)) // id past the initial length forces a grow

	if !s.has(VFS(100)) {
		t.Fatal("grown id missing")
	}

	if s.has(VFS(99)) {
		t.Fatal("phantom member after grow")
	}
}

func TestIdSet_HasOutOfRange(t *testing.T) {
	var s IdSet
	s.reset(4)

	if s.has(VFS(1000)) {
		t.Fatal("out-of-range id reported present")
	}
}

func TestIdSet_ResetGrowsAndClears(t *testing.T) {
	var s IdSet
	s.reset(4)
	s.add(VFS(2))
	s.reset(64) // larger than current length: realloc, epoch back to 1

	if s.has(VFS(2)) {
		t.Fatal("member survived grow-reset")
	}

	s.add(VFS(50))

	if !s.has(VFS(50)) {
		t.Fatal("member missing after grow-reset")
	}
}

func TestIdSet_EpochWraparoundZeroes(t *testing.T) {
	var s IdSet
	s.reset(8)

	// Drive the epoch to overflow on the next reset: reset does epoch++ and, when
	// that wraps to 0, zeroes gen and restarts at epoch 1. Stamp a stale slot with
	// the pre-wrap epoch so it would falsely match if gen were NOT zeroed.
	s.epoch = 0xFFFFFFFF
	s.gen[3] = 0xFFFFFFFF

	s.reset(8)

	if s.epoch != 1 {
		t.Fatalf("epoch after wraparound = %d, want 1", s.epoch)
	}

	if s.has(VFS(3)) {
		t.Fatal("stale member survived epoch wraparound")
	}

	s.add(VFS(3))

	if !s.has(VFS(3)) {
		t.Fatal("re-add after wraparound missing")
	}
}

func TestIdSet_SpliceNew(t *testing.T) {
	var s IdSet
	s.reset(64)
	s.add(VFS(5))

	block := make([]VFS, 8)
	block[0] = VFS(1)
	// 5 is present (skipped), 3 appended once despite the in-window repeat.
	k := s.spliceNew([]VFS{VFS(5), VFS(3), VFS(3), VFS(7)}, block, 1)

	if k != 3 || block[1] != VFS(3) || block[2] != VFS(7) {
		t.Fatalf("spliceNew k=%d block=%v", k, block[:k])
	}

	if !s.has(VFS(3)) || !s.has(VFS(7)) {
		t.Fatal("spliced ids not stamped present")
	}
}

func TestIdSet_SpliceNewOutOfBoundPanics(t *testing.T) {
	// Callers presize via reset(vfsBound()); an id past the bound means the
	// invariant broke — the runtime bounds check must fail fast, not grow.
	var s IdSet
	s.reset(4)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on out-of-bound splice id")
		}
	}()

	block := make([]VFS, 4)
	s.spliceNew([]VFS{VFS(1000)}, block, 0)
}
