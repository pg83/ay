package main

// deDuper dedups VFS slices via an epoch-stamped idSet instead of a fresh map per
// call — the dense array is reused across calls (only the epoch bumps), killing the
// per-call seen-map churn. Single-threaded use only (one idSet, reset per call).
type deDuper struct {
	seen idSet
}

// deduper is the program-global VFS deduper. gen runs single-threaded and every
// dedup is a leaf (reset → scan → return) with no re-entrancy, so one shared
// idSet backs the free-function dedupVFS, the genModule peer-collection passes,
// and the codegen dep-ref dedup alike. The intern table is global on the same
// single-gen-at-a-time assumption.
var deduper deDuper

// reset clears the deduper for a fresh single-set pass: callers then dedup an
// incrementally-built set via add (one logical set per reset). Used by
// genModule's peer-collection passes, which each reset then stream one set
// through add — reusing this one run-wide idSet instead of a map per set.
func (dd *deDuper) reset() {
	dd.seen.reset(vfsBound())
}

// add reports whether v was newly added (absent before this call) since the last
// reset; a false return means v is a duplicate within the current set.
func (dd *deDuper) add(v VFS) bool {
	if dd.seen.has(v) {
		return false
	}

	dd.seen.add(v)

	return true
}

func (dd *deDuper) dedupVFS(lists ...[]VFS) []VFS {
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
// first-occurrence order. It routes through the program-global deduper (an epoch
// idSet reused across every call) instead of allocating a fresh map: gen is
// single-threaded and each call is a leaf (reset → scan → return), so the one
// shared idSet is safe.
func dedupVFS(lists ...[]VFS) []VFS {
	return deduper.dedupVFS(lists...)
}
