package main

var (
	dedupers     DeDuperPool
	vfsScratches VFSScratchPool
)

type DeDuperPool struct {
	free []*DeDuper
}

func (p *DeDuperPool) get() *DeDuper {
	if n := len(p.free); n > 0 {
		d := p.free[n-1]

		p.free = p.free[:n-1]

		d.reset()

		return d
	}

	d := &DeDuper{}

	d.reset()

	return d
}

func (p *DeDuperPool) put(d *DeDuper) {
	p.free = append(p.free, d)
}

type IdKey interface {
	~uint32
	strID() uint32
}

type DeDuper struct {
	gen   Vec[uint32]
	epoch uint32
}

func (dd *DeDuper) reset() {
	if dd.gen.freshLen(int(vfsBound())) {
		dd.epoch = 1

		return
	}

	dd.epoch++

	if dd.epoch == 0 {
		clear(dd.gen.s)

		dd.epoch = 1
	}
}

func (dd *DeDuper) add(id uint32) bool {
	dd.gen.ensureLen(int(id) + 1)

	if dd.gen.s[id] == dd.epoch {
		return false
	}

	dd.gen.s[id] = dd.epoch

	return true
}

func (dd *DeDuper) has(id uint32) bool {
	return dd.gen.s[id] == dd.epoch
}

func (dd *DeDuper) filterSeen(na *NodeArenas, list []VFS) []VFS {
	for i, v := range list {
		if dd.add(v.strID()) {
			continue
		}

		out := na.vfs.alloc(len(list) - 1)[:0]

		out = append(out, list[:i]...)

		for _, w := range list[i+1:] {
			if dd.add(w.strID()) {
				out = append(out, w)
			}
		}

		na.vfs.commit(len(out))

		return out[:len(out):len(out)]
	}

	return list
}

func dedupClosure(na *NodeArenas, extra []VFS, groups ...[][]VFS) []VFS {
	total := len(extra)

	for _, g := range groups {
		for _, b := range g {
			total += len(b)
		}
	}

	if total == 0 {
		return nil
	}

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	out := na.vfs.alloc(total)[:0]

	for _, v := range extra {
		if deduper.add(v.strID()) {
			out = append(out, v)
		}
	}

	for _, g := range groups {
		for _, b := range g {
			for _, v := range b {
				if deduper.add(v.strID()) {
					out = append(out, v)
				}
			}
		}
	}

	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

func dedupInPlace[T IdKey](xs []T) []T {
	deduper := dedupers.get()

	defer dedupers.put(deduper)

	out := xs[:0]

	for _, x := range xs {
		if deduper.add(x.strID()) {
			out = append(out, x)
		}
	}

	return out
}

func dedup[T IdKey](lists ...[]T) []T {
	total := 0

	for _, l := range lists {
		total += len(l)
	}

	if total == 0 {
		return nil
	}

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	out := make([]T, 0, total)

	for _, l := range lists {
		for _, x := range l {
			if deduper.add(x.strID()) {
				out = append(out, x)
			}
		}
	}

	return out
}

func dedupSourceVFS(na *NodeArenas, inputs []VFS, extra [][]VFS) []VFS {
	bound := len(inputs)

	for _, b := range extra {
		bound += len(b)
	}

	out := na.vfs.alloc(bound)[:0]
	deduper := dedupers.get()

	defer dedupers.put(deduper)

	keep := func(input VFS) {
		if !input.isSource() {
			return
		}

		if !deduper.add(input.strID()) {
			return
		}

		out = append(out, input)
	}

	for _, input := range inputs {
		keep(input)
	}

	eachBucketVFS(extra, keep)
	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

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
