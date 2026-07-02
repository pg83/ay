package main

import "testing"

func TestPageVec_PushReturnsSequentialIDs(t *testing.T) {
	var v PageVec[int]

	for i := 0; i < 1000; i++ {
		if id := v.push(i * 10); id != i {
			t.Fatalf("push #%d returned id %d, want %d", i, id, i)
		}
	}

	if v.len() != 1000 {
		t.Fatalf("len = %d, want 1000", v.len())
	}
}

func TestPageVec_RoundTripAcrossPageBoundaries(t *testing.T) {
	var v PageVec[uint64]
	const n = 1 << 14

	for i := 0; i < n; i++ {
		v.push(uint64(i)*3 + 1)
	}

	for i := 0; i < n; i++ {
		if got := *v.at(i); got != uint64(i)*3+1 {
			t.Fatalf("at(%d) = %d, want %d", i, got, uint64(i)*3+1)
		}
	}
}

func TestPageVec_AtReturnsMutablePointer(t *testing.T) {
	var v PageVec[int]

	for i := 0; i < 100; i++ {
		v.push(i)
	}

	*v.at(42) = 999

	if got := *v.at(42); got != 999 {
		t.Fatalf("at(42) after mutation = %d, want 999", got)
	}
}

func TestPageVec_StructElementFields(t *testing.T) {
	type cell struct {
		s  string
		lo uint64
	}

	var v PageVec[cell]

	for i := 0; i < 500; i++ {
		v.push(cell{s: "x", lo: uint64(i)})
	}

	e := v.at(300)

	if e.s != "x" || e.lo != 300 {
		t.Fatalf("at(300) = %+v, want {x 300}", *e)
	}
}

func TestPageVec_LazyPageAllocation(t *testing.T) {
	var v PageVec[int]

	for _, page := range v.pages {
		if page != nil {
			t.Fatal("fresh PageVec has a non-nil page")
		}
	}

	// push 100 elements → ids 0..99, top id 99 → m=100 → page floor(log2(100))=6
	for i := 0; i < 100; i++ {
		v.push(i)
	}

	for p := 7; p < 64; p++ {
		if v.pages[p] != nil {
			t.Fatalf("page %d allocated without any element reaching it", p)
		}
	}

	for p := 0; p <= 6; p++ {
		if int64(len(v.pages[p])) != int64(1)<<uint(p) {
			t.Fatalf("page %d len = %d, want %d", p, len(v.pages[p]), int64(1)<<uint(p))
		}
	}
}

func TestPageVec_PageBoundaryValues(t *testing.T) {
	var v PageVec[int]
	const n = 1 << 12

	for i := 0; i < n; i++ {
		v.push(i)
	}

	// exercise exact power-of-two boundary ids where a new page begins
	for p := 0; (1 << p) < n; p++ {
		id := (1 << p) - 1 // first id of page p is m=2^p → id=2^p-1

		if got := *v.at(id); got != id {
			t.Fatalf("boundary at(%d) = %d, want %d", id, got, id)
		}
	}
}
