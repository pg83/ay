package main

type DeDuper struct {
	seen IdSet
}

var deduper DeDuper

func (dd *DeDuper) reset() {
	dd.seen.reset(vfsBound())
}

func (dd *DeDuper) add(v VFS) bool {
	if dd.seen.has(v) {
		return false
	}

	dd.seen.add(v)

	return true
}

func (dd *DeDuper) has(v VFS) bool {
	return dd.seen.has(v)
}

func (dd *DeDuper) filterSeen(list []VFS) []VFS {
	for i, v := range list {
		if dd.add(v) {
			continue
		}

		out := append(make([]VFS, 0, len(list)-1), list[:i]...)

		for _, w := range list[i+1:] {
			if dd.add(w) {
				out = append(out, w)
			}
		}

		return out
	}

	return list
}

func (dd *DeDuper) dedupVFS(lists ...[]VFS) []VFS {
	total := 0

	for _, l := range lists {
		total += len(l)
	}

	dd.seen.reset(vfsBound())
	out := make([]VFS, 0, total)

	for _, l := range lists {
		for _, x := range l {
			if dd.seen.has(x) {
				continue
			}

			dd.seen.add(x)
			out = append(out, x)
		}
	}

	return out
}

func dedupVFS(lists ...[]VFS) []VFS {
	return deduper.dedupVFS(lists...)
}

func concat[T any](lists ...[]T) []T {
	total := 0

	for _, l := range lists {
		total += len(l)
	}

	if total == 0 {
		return nil
	}

	out := make([]T, 0, total)

	for _, l := range lists {
		out = append(out, l...)
	}

	return out
}
