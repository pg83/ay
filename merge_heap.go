package main

import (
	"bufio"
	"io"
)

type MergeItem struct {
	line   string
	reader *bufio.Reader
	closer io.Closer
}

type MergeHeap []*MergeItem

func (h MergeHeap) len() int {
	return len(h)
}

// Len implements container/heap.Interface.
func (h MergeHeap) Len() int {
	return h.len()
}

func (h MergeHeap) less(i, j int) bool {
	return h[i].line < h[j].line
}

// Less implements container/heap.Interface.
func (h MergeHeap) Less(i, j int) bool {
	return h.less(i, j)
}

func (h MergeHeap) swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Swap implements container/heap.Interface.
func (h MergeHeap) Swap(i, j int) {
	h.swap(i, j)
}

func (h *MergeHeap) push(x any) {
	*h = append(*h, x.(*MergeItem))
}

// Push implements container/heap.Interface.
func (h *MergeHeap) Push(x any) {
	h.push(x)
}

func (h *MergeHeap) pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]

	return it
}

// Pop implements container/heap.Interface.
func (h *MergeHeap) Pop() any {
	return h.pop()
}
