package main

var (
	vfsScratches     = newSliceScratchPool[VFS](1 << 10)
	nodeRefScratches = newSliceScratchPool[NodeRef](1 << 10)
)

type SliceScratchPool[T any] struct {
	free [][]T
	hint int
}

func newSliceScratchPool[T any](hint int) SliceScratchPool[T] {
	return SliceScratchPool[T]{hint: hint}
}

func (p *SliceScratchPool[T]) get() []T {
	if n := len(p.free); n > 0 {
		s := p.free[n-1]

		p.free = p.free[:n-1]

		return s[:0]
	}

	return make([]T, 0, p.hint)
}

func (p *SliceScratchPool[T]) put(s []T) {
	p.free = append(p.free, s)
}
