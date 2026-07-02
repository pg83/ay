package main

import "testing"

func vid(n uint32) VFS {
	return VFS(n << 1)
}

func TestIdSet_AddHas(t *testing.T) {
	var s IdSet
	s.reset(8)

	if s.has(vid(3)) {
		t.Fatal("fresh set reports a member")
	}

	s.add(vid(3))
	s.add(vid(5))

	if !s.has(vid(3)) || !s.has(vid(5)) {
		t.Fatal("added ids not present")
	}

	if s.has(vid(4)) {
		t.Fatal("never-added id present")
	}
}

func TestIdSet_ResetClearsMembershipReusingArray(t *testing.T) {
	var s IdSet
	s.reset(8)
	s.add(vid(2))

	if !s.has(vid(2)) {
		t.Fatal("member missing before reset")
	}

	before := cap(s.gen)
	s.reset(8)

	if s.has(vid(2)) {
		t.Fatal("member survived reset")
	}

	if cap(s.gen) != before {
		t.Fatalf("reset reallocated backing array (cap %d -> %d) for an unchanged size", before, cap(s.gen))
	}
}

func TestIdSet_AddGrowsBeyondLen(t *testing.T) {
	var s IdSet
	s.reset(4)
	s.add(vid(100))

	if !s.has(vid(100)) {
		t.Fatal("grown id missing")
	}

	if s.has(vid(99)) {
		t.Fatal("phantom member after grow")
	}
}

func TestIdSet_HasOutOfRange(t *testing.T) {
	var s IdSet
	s.reset(4)

	if s.has(vid(1000)) {
		t.Fatal("out-of-range id reported present")
	}
}

func TestIdSet_ResetGrowsAndClears(t *testing.T) {
	var s IdSet
	s.reset(4)
	s.add(vid(2))
	s.reset(64)

	if s.has(vid(2)) {
		t.Fatal("member survived grow-reset")
	}

	s.add(vid(50))

	if !s.has(vid(50)) {
		t.Fatal("member missing after grow-reset")
	}
}

func TestIdSet_EpochWraparoundZeroes(t *testing.T) {
	var s IdSet
	s.reset(8)

	s.epoch = 0xFFFF
	s.gen[3] = 0xFFFF

	s.reset(8)

	if s.epoch != 1 {
		t.Fatalf("epoch after wraparound = %d, want 1", s.epoch)
	}

	if s.has(vid(3)) {
		t.Fatal("stale member survived epoch wraparound")
	}

	s.add(vid(3))

	if !s.has(vid(3)) {
		t.Fatal("re-add after wraparound missing")
	}
}

func TestIdSet_SpliceNew(t *testing.T) {
	var s IdSet
	s.reset(64)
	s.add(vid(5))

	block := make([]VFS, 8)
	block[0] = vid(1)

	k := s.spliceNew([]VFS{vid(5), vid(3), vid(3), vid(7)}, block, 1)

	if k != 3 || block[1] != vid(3) || block[2] != vid(7) {
		t.Fatalf("spliceNew k=%d block=%v", k, block[:k])
	}

	if !s.has(vid(3)) || !s.has(vid(7)) {
		t.Fatal("spliced ids not stamped present")
	}
}

func TestIdSet_SpliceNewOutOfBoundPanics(t *testing.T) {
	var s IdSet
	s.reset(4)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on out-of-bound splice id")
		}
	}()

	block := make([]VFS, 4)
	s.spliceNew([]VFS{vid(1000)}, block, 0)
}
