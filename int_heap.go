package main

type IntHeap []int

func (h IntHeap) len() int {
	return len(h)
}

// Len implements container/heap.Interface (its sort.Interface part).
func (h IntHeap) Len() int {
	return h.len()
}

func (h IntHeap) less(i, j int) bool {
	return h[i] < h[j]
}

// Less implements container/heap.Interface (its sort.Interface part).
func (h IntHeap) Less(i, j int) bool {
	return h.less(i, j)
}

func (h IntHeap) swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Swap implements container/heap.Interface (its sort.Interface part).
func (h IntHeap) Swap(i, j int) {
	h.swap(i, j)
}

func (h *IntHeap) push(x interface{}) {
	*h = append(*h, x.(int))
}

// Push implements container/heap.Interface; the heap machinery finds it by name.
func (h *IntHeap) Push(x interface{}) {
	h.push(x)
}

func (h *IntHeap) pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]

	return x
}

// Pop implements container/heap.Interface; the heap machinery finds it by name.
func (h *IntHeap) Pop() interface{} {
	return h.pop()
}
