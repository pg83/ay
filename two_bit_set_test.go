package main

import "testing"

func TestTwoBitSet_ZeroValueReadsZero(t *testing.T) {
	var b TwoBitSet

	for _, v := range []uint32{0, 1, 31, 32, 1 << 20} {
		if got := b.get(v); got != 0 {
			t.Fatalf("get(%d) on zero value = %d, want 0", v, got)
		}
	}
}

func TestTwoBitSet_SetGetAllValues(t *testing.T) {
	var b TwoBitSet

	for val := uint8(0); val < 4; val++ {
		v := uint32(val) * 7
		b.set(v, val)

		if got := b.get(v); got != val {
			t.Fatalf("get(%d) = %d, want %d", v, got, val)
		}
	}
}

func TestTwoBitSet_OverwriteCell(t *testing.T) {
	var b TwoBitSet

	b.set(5, 3)
	b.set(5, 1)

	if got := b.get(5); got != 1 {
		t.Fatalf("get(5) after overwrite = %d, want 1", got)
	}

	b.set(5, 0)

	if got := b.get(5); got != 0 {
		t.Fatalf("get(5) after clearing = %d, want 0", got)
	}
}

func TestTwoBitSet_NeighborCellsIndependent(t *testing.T) {
	var b TwoBitSet

	for v := uint32(0); v < 32; v++ {
		b.set(v, uint8(v%4))
	}

	for v := uint32(0); v < 32; v++ {
		if got := b.get(v); got != uint8(v%4) {
			t.Fatalf("get(%d) = %d, want %d", v, got, v%4)
		}
	}

	b.set(17, 3)

	for v := uint32(0); v < 32; v++ {
		want := uint8(v % 4)

		if v == 17 {
			want = 3
		}

		if got := b.get(v); got != want {
			t.Fatalf("get(%d) after set(17) = %d, want %d", v, got, want)
		}
	}
}

func TestTwoBitSet_GrowsAcrossWords(t *testing.T) {
	var b TwoBitSet

	ids := []uint32{0, 31, 32, 63, 64, 1000, 1 << 16}

	for i, v := range ids {
		b.set(v, uint8(i%3)+1)
	}

	for i, v := range ids {
		if got, want := b.get(v), uint8(i%3)+1; got != want {
			t.Fatalf("get(%d) = %d, want %d", v, got, want)
		}
	}
}

func TestTwoBitSet_ValueMaskedToTwoBits(t *testing.T) {
	var b TwoBitSet

	b.set(2, 0xFF)

	if got := b.get(2); got != 3 {
		t.Fatalf("get(2) after set(2, 0xFF) = %d, want 3", got)
	}

	if got := b.get(1); got != 0 {
		t.Fatalf("get(1) clobbered by masked set: %d, want 0", got)
	}

	if got := b.get(3); got != 0 {
		t.Fatalf("get(3) clobbered by masked set: %d, want 0", got)
	}
}
