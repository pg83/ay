package main

var vfsScratches VFSScratchPool

type VFSScratchPool struct {
	free [][]VFS
}

func (p *VFSScratchPool) get() []VFS {
	if n := len(p.free); n > 0 {
		s := p.free[n-1]

		p.free = p.free[:n-1]

		return s[:0]
	}

	return make([]VFS, 0, 1<<10)
}

func (p *VFSScratchPool) put(s []VFS) {
	p.free = append(p.free, s)
}
