package main

import "testing"

func TestSliceCache_InternSharesEqualContent(t *testing.T) {
	c := newSliceCache[VFS](8)

	b1 := c.alloc(3)
	b1[0], b1[1], b1[2] = 10, 20, 30
	s1 := c.intern(b1[:3])

	b2 := c.alloc(3)
	b2[0], b2[1], b2[2] = 10, 20, 30
	s2 := c.intern(b2[:3])

	if &s1[0] != &s2[0] {
		t.Fatalf("equal content not shared: %p vs %p", &s1[0], &s2[0])
	}
}

func TestSliceCache_OrderMatters(t *testing.T) {
	c := newSliceCache[VFS](8)

	b1 := c.alloc(2)
	b1[0], b1[1] = 10, 20
	s1 := c.intern(b1[:2])

	b2 := c.alloc(2)
	b2[0], b2[1] = 20, 10
	s2 := c.intern(b2[:2])

	if &s1[0] == &s2[0] {
		t.Fatalf("different order must not share backing")
	}

	if s1[0] != 10 || s2[0] != 20 {
		t.Fatalf("contents corrupted: %v %v", s1, s2)
	}
}

func TestSliceCache_HitDoesNotCommit(t *testing.T) {
	c := newSliceCache[VFS](8)

	b1 := c.alloc(2)
	b1[0], b1[1] = 1, 2
	c.intern(b1[:2])

	b2 := c.alloc(2)
	b2[0], b2[1] = 1, 2
	c.intern(b2[:2])

	b3 := c.alloc(2)

	if &b2[0] != &b3[0] {
		t.Fatalf("hit must leave the block uncommitted for reuse")
	}
}

func TestSliceCache_InternEmptyIsNil(t *testing.T) {
	c := newSliceCache[VFS](8)

	if got := c.intern(nil); got != nil {
		t.Fatalf("intern(nil) = %v, want nil", got)
	}
}

func TestSliceCache_InternedCapIsClamped(t *testing.T) {
	c := newSliceCache[VFS](8)

	b := c.alloc(2)
	b[0], b[1] = 7, 8
	s := c.intern(b[:2])

	if cap(s) != 2 {
		t.Fatalf("cap = %d, want 2 (append must copy, not clobber shared backing)", cap(s))
	}
}

func TestSliceCache_DedupShared(t *testing.T) {
	c := newSliceCache[VFS](8)

	s := dedupShared(c, []VFS{2, 4, 2}, []VFS{4, 6})

	want := []VFS{2, 4, 6}

	if len(s) != len(want) {
		t.Fatalf("dedupShared = %v, want %v", s, want)
	}

	for i, v := range want {
		if s[i] != v {
			t.Fatalf("dedupShared = %v, want %v", s, want)
		}
	}

	s2 := dedupShared(c, []VFS{2}, []VFS{4, 6, 2, 4})

	if &s[0] != &s2[0] {
		t.Fatalf("same dedup result must share backing")
	}
}

func TestSliceCache_DedupSharedSingletonIsShared(t *testing.T) {
	c := newSliceCache[VFS](8)
	in := []VFS{2}
	out := dedupShared(c, nil, in)

	if &out[0] != &in[0] {
		t.Fatal("single input element was copied")
	}
}

func TestSliceCache_ConcatSharedSingleInputIsShared(t *testing.T) {
	c := newSliceCache[ARG](8)

	in := []ARG{5, 5, 7}

	if s := concatShared(c, nil, in); &s[0] != &in[0] {
		t.Fatalf("single non-empty input must be returned as-is")
	}

	both := concatShared(c, []ARG{1}, in)
	want := []ARG{1, 5, 5, 7}

	for i, v := range want {
		if both[i] != v {
			t.Fatalf("concatShared = %v, want %v", both, want)
		}
	}
}
