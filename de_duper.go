package main

var deduper DeDuper

type idKey interface {
	~uint32
	strID() uint32
}

type DeDuper struct {
	gen   []uint32
	epoch uint32
}

func (dd *DeDuper) reset() {
	size := strBound()

	if uint32(len(dd.gen)) < size {
		grown := uint32(len(dd.gen)) * 2

		if grown < size {
			grown = size
		}

		dd.gen = make([]uint32, grown)
		dd.epoch = 1

		return
	}

	dd.epoch++

	if dd.epoch == 0 {
		for i := range dd.gen {
			dd.gen[i] = 0
		}

		dd.epoch = 1
	}
}

func (dd *DeDuper) add(id uint32) bool {
	if dd.gen[id] == dd.epoch {
		return false
	}

	dd.gen[id] = dd.epoch

	return true
}

func (dd *DeDuper) has(id uint32) bool {
	return dd.gen[id] == dd.epoch
}

func (dd *DeDuper) filterSeen(list []VFS) []VFS {
	for i, v := range list {
		if dd.add(v.strID()) {
			continue
		}

		out := append(make([]VFS, 0, len(list)-1), list[:i]...)

		for _, w := range list[i+1:] {
			if dd.add(w.strID()) {
				out = append(out, w)
			}
		}

		return out
	}

	return list
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

func containsVFS(xs []VFS, want VFS) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}

	return false
}
