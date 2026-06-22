package main

import (
	"container/heap"
	"math/rand/v2"
	"sort"
	"testing"
)

func popAllInts(h *IntHeap) []int {
	out := make([]int, 0, h.len())

	for h.len() > 0 {
		out = append(out, heap.Pop(h).(int))
	}

	return out
}

func TestIntHeap_PopsAscending(t *testing.T) {
	h := &IntHeap{}

	for _, v := range []int{42, 7, 19, 7, 0, -3} {
		heap.Push(h, v)
	}

	got := popAllInts(h)
	want := []int{-3, 0, 7, 7, 19, 42}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pop order = %v, want %v", got, want)
		}
	}
}

func TestIntHeap_InitHeapifiesExistingSlice(t *testing.T) {
	h := &IntHeap{5, 1, 4, 2, 3}

	heap.Init(h)

	got := popAllInts(h)

	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("not ascending: %v", got)
		}
	}

	if len(got) != 5 {
		t.Fatalf("drained %d elements, want 5", len(got))
	}
}

func TestIntHeap_RandomizedMatchesSort(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	h := &IntHeap{}
	ref := make([]int, 0, 1000)

	for i := 0; i < 1000; i++ {
		v := int(r.Int32N(100))
		heap.Push(h, v)
		ref = append(ref, v)
	}

	sort.Ints(ref)

	got := popAllInts(h)

	for i := range ref {
		if got[i] != ref[i] {
			t.Fatalf("mismatch at %d: got %d, want %d", i, got[i], ref[i])
		}
	}
}

// Exported wrappers exist for container/heap dispatch; they delegate to the
// lower-case twins.
func TestIntHeap_WrappersDelegate(t *testing.T) {
	h := IntHeap{2, 1}

	if h.Len() != h.len() || h.Len() != 2 {
		t.Fatalf("Len = %d / len = %d, want 2", h.Len(), h.len())
	}

	if !h.Less(1, 0) || h.less(0, 1) {
		t.Fatalf("Less/less disagree: Less(1,0)=%v less(0,1)=%v", h.Less(1, 0), h.less(0, 1))
	}

	h.Swap(0, 1)

	if h[0] != 1 || h[1] != 2 {
		t.Fatalf("Swap did not swap: %v", h)
	}
}
