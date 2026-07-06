package main

import "testing"

func TestVec_PushBackDoubles(t *testing.T) {
	var v Vec[int]

	caps := map[int]bool{}

	for i := 0; i < 1000; i++ {
		v.pushBack(i)
		caps[cap(v.s)] = true
	}

	if v.len() != 1000 {
		t.Fatalf("len = %d, want 1000", v.len())
	}

	for i, x := range v.s {
		if x != i {
			t.Fatalf("s[%d] = %d", i, x)
		}
	}

	for c := range caps {
		if c != 0 && (c&(c-1)) != 0 {
			t.Fatalf("cap %d is not a power of two — growth is not doubling", c)
		}
	}
}

func TestVec_LargeVecGrowsSlower(t *testing.T) {
	var v Vec[uint64]

	v.reserve(vecSlowGrowBytes / 8)

	c0 := cap(v.s)

	v.s = v.s[:c0]
	v.pushBack(1)

	if c1 := cap(v.s); c1 >= c0*2 {
		t.Fatalf("large vec doubled: %d -> %d, want 1.5x", c0, c1)
	}
}

func TestVec_ReserveAvoidsRegrowth(t *testing.T) {
	var v Vec[int]

	v.reserve(100)

	p := &v.s[:1][0]

	for i := 0; i < 100; i++ {
		v.pushBack(i)
	}

	if &v.s[0] != p {
		t.Fatalf("backing array moved despite reserve")
	}
}

func TestVec_EnsureLenZeroFills(t *testing.T) {
	var v Vec[int]

	v.pushBack(7)
	v.ensureLen(50)

	if v.len() != 50 {
		t.Fatalf("len = %d, want 50", v.len())
	}

	if v.s[0] != 7 {
		t.Fatalf("existing element lost: %d", v.s[0])
	}

	for i := 1; i < 50; i++ {
		if v.s[i] != 0 {
			t.Fatalf("s[%d] = %d, want 0", i, v.s[i])
		}
	}

	v.ensureLen(10)

	if v.len() != 50 {
		t.Fatalf("ensureLen must never shrink: len = %d", v.len())
	}
}
