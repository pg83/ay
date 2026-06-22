package main

// DeDuper dedups VFS slices via an epoch-stamped IdSet reused across calls,
// avoiding per-call seen-map churn. Single-threaded use only.
type DeDuper struct {
	seen IdSet
}

// deduper is the program-global VFS deduper: gen is single-threaded and every
// dedup is a non-re-entrant leaf, so one shared IdSet backs every caller.
var deduper DeDuper

// reset clears the deduper for a fresh single-set pass (one logical set per reset).
func (dd *DeDuper) reset() {
	dd.seen.reset(vfsBound())
}

// add reports whether v was newly added since the last reset; false means a
// duplicate within the current set.
func (dd *DeDuper) add(v VFS) bool {
	if dd.seen.has(v) {
		return false
	}

	dd.seen.add(v)

	return true
}

// has reports whether v was added since the last reset.
func (dd *DeDuper) has(v VFS) bool {
	return dd.seen.has(v)
}

// filterSeen drops elements already in the current set, preserving order.
// Copy-on-write: the input slice is returned as-is when nothing is dropped; a
// fresh slice is built only on the first dup.
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

// dedupVFS unions the given VFS lists, dropping duplicates, preserving
// first-occurrence order, routing through the program-global deduper instead of
// allocating a fresh map.
func dedupVFS(lists ...[]VFS) []VFS {
	return deduper.dedupVFS(lists...)
}
