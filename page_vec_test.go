package main

import "testing"

func TestPageVec_OffsetLayoutSmall(t *testing.T) {
	cases := []struct {
		id   uint32
		page int
		off  int64
	}{
		{0, 0, 0},
		{1, 1, 0},
		{2, 1, 1},
		{3, 2, 0},
		{4, 2, 1},
		{5, 2, 2},
		{6, 2, 3},
		{7, 3, 0},
		{14, 3, 7},
		{15, 4, 0},
	}

	for _, c := range cases {
		p, off := pageOffset(c.id)

		if p != c.page || off != c.off {
			t.Errorf("pageOffset(%d) = (%d, %d), want (%d, %d)", c.id, p, off, c.page, c.off)
		}
	}
}

func TestPageVec_OffsetBijection(t *testing.T) {
	ids := []uint32{}

	for i := uint32(0); i < 5000; i++ {
		ids = append(ids, i)
	}

	for p := 0; p < 31; p++ {
		base := uint32(1)<<uint(p) - 1
		ids = append(ids, base, base+1)
	}

	ids = append(ids, 1<<31-1, 1<<31, 1<<20, 1<<20+1, 3000000000)

	for _, id := range ids {
		p, off := pageOffset(id)

		if p < 0 || p > 31 {
			t.Fatalf("pageOffset(%d) page %d out of [0,31]", id, p)
		}

		size := int64(1) << uint(p)

		if off < 0 || off >= size {
			t.Errorf("pageOffset(%d) off %d out of page size %d", id, off, size)
		}

		if got := uint32(size-1) + uint32(off); got != id {
			t.Errorf("reconstruct id from (%d,%d) = %d, want %d", p, off, got, id)
		}
	}
}

func TestPageVec_SetGetRoundTrip(t *testing.T) {
	var v PageVec[int]

	ids := []uint32{0, 1, 2, 3, 6, 7, 8, 15, 16, 31, 32, 1000, 1<<16 - 1, 1 << 16, 1 << 20, 1<<21 + 123}

	for _, id := range ids {
		v.set(id, int(id)*3+1)
	}

	for _, id := range ids {
		if got := v.get(id); got != int(id)*3+1 {
			t.Errorf("get(%d) = %d, want %d", id, got, int(id)*3+1)
		}
	}
}

func TestPageVec_NoAliasing(t *testing.T) {
	var v PageVec[uint32]

	const n = 20000

	for id := uint32(0); id < n; id++ {
		v.set(id, id)
	}

	for id := uint32(0); id < n; id++ {
		if got := v.get(id); got != id {
			t.Fatalf("get(%d) = %d — slot aliased", id, got)
		}
	}
}

func TestPageVec_LazyPageSizes(t *testing.T) {
	var v PageVec[int]

	if v.pages[3].Load() != nil {
		t.Fatalf("page 3 allocated before any set")
	}

	v.set(10, 42)

	page := v.pages[3].Load()

	if page == nil {
		t.Fatalf("id 10 lives in page 3, page not allocated")
	}

	if len(*page) != 1<<3 {
		t.Errorf("page 3 size = %d, want %d", len(*page), 1<<3)
	}

	for p := range v.pages {
		if p != 3 && v.pages[p].Load() != nil {
			t.Errorf("page %d unexpectedly allocated", p)
		}
	}
}

func TestPageVec_GetSafeUnsetIsZero(t *testing.T) {
	var v PageVec[string]

	if got := v.getSafe(1000); got != "" {
		t.Errorf("getSafe on unset page = %q, want empty", got)
	}

	v.set(1000, "x")

	if got := v.getSafe(1000); got != "x" {
		t.Errorf("getSafe(1000) = %q, want %q", got, "x")
	}

	if got := v.getSafe(1001); got != "" {
		t.Errorf("getSafe on unset slot in allocated page = %q, want empty", got)
	}
}
