package main

import "testing"

func TestBitSet_EmptyHasNothing(t *testing.T) {
	var b BitSet

	for _, v := range []uint32{0, 1, 63, 64, 65, 1000, 1 << 20} {
		if b.has(v) {
			t.Errorf("empty set reports %d present", v)
		}
	}
}

func TestBitSet_AddHas(t *testing.T) {
	var b BitSet

	ids := []uint32{0, 1, 63, 64, 65, 127, 128, 4095, 100000}

	for _, v := range ids {
		b.add(v)
	}

	for _, v := range ids {
		if !b.has(v) {
			t.Errorf("added %d not present", v)
		}
	}

	// Neighbours of set bits stay clear.
	for _, v := range []uint32{2, 62, 66, 129, 99999, 100001} {
		if b.has(v) {
			t.Errorf("unset %d reported present", v)
		}
	}
}

func TestBitSet_WordBoundary(t *testing.T) {
	var b BitSet

	// Bits 63 and 64 fall in different words: no cross-talk.
	b.add(63)

	if !b.has(63) || b.has(64) {
		t.Fatalf("63/64 boundary wrong: has63=%v has64=%v", b.has(63), b.has(64))
	}

	b.add(64)

	if !b.has(63) || !b.has(64) {
		t.Fatalf("after add(64): has63=%v has64=%v", b.has(63), b.has(64))
	}
}

func TestBitSet_GrowsPreservingEarlierBits(t *testing.T) {
	var b BitSet

	b.add(5)
	b.add(1 << 20) // grows the backing slice

	if !b.has(5) {
		t.Error("growth dropped earlier bit 5")
	}

	if !b.has(1 << 20) {
		t.Error("bit 1<<20 absent after growth")
	}
}

func TestBitSet_AddIsIdempotent(t *testing.T) {
	var b BitSet

	b.add(42)
	b.add(42)

	if !b.has(42) {
		t.Error("double add lost the bit")
	}
}

func TestBitSet_DenseFillRoundTrips(t *testing.T) {
	var b BitSet

	const n = 5000

	for i := uint32(0); i < n; i += 3 {
		b.add(i)
	}

	for i := uint32(0); i < n; i++ {
		want := i%3 == 0

		if got := b.has(i); got != want {
			t.Fatalf("id %d: has=%v want=%v", i, got, want)
		}
	}
}
