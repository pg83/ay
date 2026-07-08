package main

var deduper DeDuper

type idKey interface {
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

	deduper.reset()

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

func dedup[T idKey](lists ...[]T) []T {
	total := 0

	for _, l := range lists {
		total += len(l)
	}

	if total == 0 {
		return nil
	}

	deduper.reset()

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

	deduper.reset()

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

func concat[T any](lists ...[]T) []T {
	total := 0
	nonEmpty := 0
	last := -1

	for i, l := range lists {
		if len(l) > 0 {
			total += len(l)
			nonEmpty++
			last = i
		}
	}

	if total == 0 {
		return nil
	}

	if nonEmpty == 1 {
		return lists[last]
	}

	out := make([]T, 0, total)

	for _, l := range lists {
		out = append(out, l...)
	}

	return out
}
