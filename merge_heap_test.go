package main

import (
	"container/heap"
	"testing"
)

func mergeHeapOf(lines ...string) *MergeHeap {
	h := &MergeHeap{}

	for _, l := range lines {
		heap.Push(h, &MergeItem{line: l})
	}

	return h
}

func popAllMergeLines(h *MergeHeap) []string {
	out := make([]string, 0, h.len())

	for h.len() > 0 {
		out = append(out, heap.Pop(h).(*MergeItem).line)
	}

	return out
}

func TestMergeHeap_PopsLinesInLexicographicOrder(t *testing.T) {
	h := mergeHeapOf("delta", "alpha", "charlie", "bravo")

	got := popAllMergeLines(h)
	want := []string{"alpha", "bravo", "charlie", "delta"}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pop order = %v, want %v", got, want)
		}
	}

	if h.len() != 0 {
		t.Fatalf("heap not drained: len = %d", h.len())
	}
}

func TestMergeHeap_DuplicateLinesAllSurvive(t *testing.T) {
	h := mergeHeapOf("x", "a", "x", "a")

	got := popAllMergeLines(h)
	want := []string{"a", "a", "x", "x"}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pop order = %v, want %v", got, want)
		}
	}
}

func TestMergeHeap_InterleavedPushPopKeepsMinAtTop(t *testing.T) {
	h := mergeHeapOf("m")

	heap.Push(h, &MergeItem{line: "z"})

	if got := heap.Pop(h).(*MergeItem).line; got != "m" {
		t.Fatalf("first pop = %q, want %q", got, "m")
	}

	heap.Push(h, &MergeItem{line: "a"})

	if got := heap.Pop(h).(*MergeItem).line; got != "a" {
		t.Fatalf("pop after pushing smaller = %q, want %q", got, "a")
	}

	if got := heap.Pop(h).(*MergeItem).line; got != "z" {
		t.Fatalf("final pop = %q, want %q", got, "z")
	}
}

// The wrappers exist for container/heap dispatch; lower-case twins implement.
func TestMergeHeap_WrappersDelegate(t *testing.T) {
	h := MergeHeap{{line: "b"}, {line: "a"}}

	if h.Len() != h.len() || h.Len() != 2 {
		t.Fatalf("Len = %d / len = %d, want 2", h.Len(), h.len())
	}

	if !h.Less(1, 0) || h.less(0, 1) {
		t.Fatalf("Less/less disagree: Less(1,0)=%v less(0,1)=%v", h.Less(1, 0), h.less(0, 1))
	}

	h.Swap(0, 1)

	if h[0].line != "a" || h[1].line != "b" {
		t.Fatalf("Swap did not swap: %q, %q", h[0].line, h[1].line)
	}
}
