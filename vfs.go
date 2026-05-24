package main

import (
	"encoding/json"
)

// vfs.go — typed VFS path.
//
// A VFS value addresses a file in one of two virtual roots (SOURCE_ROOT /
// BUILD_ROOT) by its root-relative path. The struct is comparable (map
// key / struct field); materialisation to "$(S)/<rel>" / "$(B)/<rel>"
// happens only at the serializer boundary (gjson_write.go). Source-tree
// existence and reads route through the FS abstraction (fs.go) keyed on
// the bare rel, bypassing String() entirely.

// VFSRoot identifies which root a `VFS` is anchored under.
type VFSRoot uint8

const (
	// VFSRootUnset is the zero value — a deliberate sentinel that
	// causes `VFS.String()` to panic, so accidental uninitialised
	// VFS values surface immediately rather than serialising as
	// "/<rel>" or similar.
	VFSRootUnset VFSRoot = iota
	VFSRootSource
	VFSRootBuild
)

// VFS addresses a file by (root, root-relative path).
type VFS struct {
	Root VFSRoot
	Rel  string
}

// Source constructs a SOURCE_ROOT-rooted VFS path.
func Source(rel string) VFS { return VFS{Root: VFSRootSource, Rel: rel} }

// Build constructs a BUILD_ROOT-rooted VFS path.
func Build(rel string) VFS { return VFS{Root: VFSRootBuild, Rel: rel} }

// IsSource reports whether v is anchored under SOURCE_ROOT.
func (v VFS) IsSource() bool { return v.Root == VFSRootSource }

// IsBuild reports whether v is anchored under BUILD_ROOT.
func (v VFS) IsBuild() bool { return v.Root == VFSRootBuild }

// String materialises the canonical "$(S)/<rel>" or "$(B)/<rel>" form
// used at the serializer boundary. The scanner / FS access path keys on
// the bare rel and never materialises.
//
// Panics on a zero-valued VFS. Construction MUST go through
// Source()/Build() (or struct-literal with an explicit Root).
func (v VFS) String() string {
	switch v.Root {
	case VFSRootSource:
		return "$(S)/" + v.Rel
	case VFSRootBuild:
		return "$(B)/" + v.Rel
	}
	panic("VFS.String: zero-valued VFS (missing Root)")
}

// LongString materialises the legacy raw-graph root spelling used by the
// upstream stats_uid preimage.
func (v VFS) LongString() string {
	switch v.Root {
	case VFSRootSource:
		return "$(SOURCE_ROOT)/" + v.Rel
	case VFSRootBuild:
		return "$(BUILD_ROOT)/" + v.Rel
	}
	panic("VFS.LongString: zero-valued VFS (missing Root)")
}

// ParseVFS recognises s as a "$(S)/..." or "$(B)/..."
// string and returns the corresponding VFS. Returns (zero, false) when
// s lacks both recognised prefixes — callers handling such tokens
// (e.g. compound CmdArg substrings) keep them as strings.
func ParseVFS(s string) (VFS, bool) {
	if rel, ok := trimVFSPrefix(s, Source("").String()); ok {
		return Source(rel), true
	}
	if rel, ok := trimVFSPrefix(s, Build("").String()); ok {
		return Build(rel), true
	}
	return VFS{}, false
}

// trimVFSPrefix returns (s without prefix, true) when prefix matches;
// (s, false) otherwise. Avoids the strings import dependency.
func trimVFSPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return s, false
	}
	return s[len(prefix):], true
}

// MarshalJSON makes VFS implement encoding/json.Marshaler so the
// reflection-based encoder (used only by tests and external tools)
// renders VFS as its canonical string form rather than the
// struct-field form `{"Root":1,"Rel":"..."}`. Production output goes
// through `gjson_write.go::appendVFS` which bypasses encoding/json
// entirely.
func (v VFS) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

// vfsRelsSlice materialises a []VFS as a []string of root-relative paths.
// Used at command-composition boundaries where the tool contract wants
// bare BUILD_ROOT-relative or SOURCE_ROOT-relative paths, not canonical
// `$(S)/...` / `$(B)/...` strings.
func vfsRelsSlice(vs []VFS) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Rel
	}
	return out
}

// concatVFS returns a ++ b. When either side is empty it returns the other
// directly with no copy (the common case: a module has only regular OR only
// global members). Unlike mergeDedupVFS it does NOT dedup — use only where a
// and b are disjoint; a duplicate would survive normalization (which sorts
// but does not dedup inputs) and trip the gate.
func concatVFS(a, b []VFS) []VFS {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]VFS, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// lessVFS orders VFS the way `a.String() < b.String()` would, byte-for-byte,
// without materialising the strings: `$(B)/<rel>` sorts before `$(S)/<rel>`
// (B < S), and within the same Root the trailing Rel orders lexicographically.
func lessVFS(a, b VFS) bool {
	if a.Root != b.Root {
		return a.Root == VFSRootBuild
	}
	return a.Rel < b.Rel
}

// VFSMap is a two-bucket map keyed by VFS (Source → 0, Build → 1) that
// stores values under the rel-string. Routes lookups through Go's
// mapaccess2_faststr (specialised for map[string]T) instead of the generic
// struct-keyed path — 2.5–2.8× faster in scanner DFS hot loops (profiled
// 373ms faststr vs 1047ms generic on the same workload; bench at 9.1 ns/op
// beats map[VFS]T at 11.9 and map[string]T-on-materialised at 10.6). The
// two buckets are exposed as a [2]-array so callers can range deterministically.
type VFSMap[T any] [2]map[string]T

// NewVFSMap constructs a VFSMap with each bucket pre-sized to `cap`.
func NewVFSMap[T any](cap int) VFSMap[T] {
	return VFSMap[T]{
		make(map[string]T, cap),
		make(map[string]T, cap),
	}
}

// vfsBucket returns the per-root bucket index for v. Panics on
// VFSRootUnset (uint8 underflow → 255 → bounds-check panic).
func vfsBucket(v VFS) uint8 { return uint8(v.Root) - 1 }

// Get returns the value stored under v and a presence flag.
func (m VFSMap[T]) Get(v VFS) (T, bool) {
	val, ok := m[vfsBucket(v)][v.Rel]
	return val, ok
}

// Set stores val under v.
func (m VFSMap[T]) Set(v VFS, val T) { m[vfsBucket(v)][v.Rel] = val }

// Has reports presence.
func (m VFSMap[T]) Has(v VFS) bool {
	_, ok := m[vfsBucket(v)][v.Rel]
	return ok
}

// Delete removes v's entry (no-op when absent).
func (m VFSMap[T]) Delete(v VFS) { delete(m[vfsBucket(v)], v.Rel) }

// Len returns the total entry count across both buckets.
func (m VFSMap[T]) Len() int { return len(m[0]) + len(m[1]) }

// Clear drops every entry in both buckets, retaining the underlying
// bucket allocations for reuse (used by the scanner sync.Pool path).
func (m VFSMap[T]) Clear() {
	clear(m[0])
	clear(m[1])
}

// VFSSet is a presence-only specialisation of VFSMap with a
// zero-byte value type. Common for DFS visited-sets.
type VFSSet = VFSMap[struct{}]

// NewVFSSet constructs a VFSSet with each bucket pre-sized to `cap`.
func NewVFSSet(cap int) VFSSet {
	return VFSSet{
		make(map[string]struct{}, cap),
		make(map[string]struct{}, cap),
	}
}

// Add inserts v into the set.
func (m VFSMap[T]) Add(v VFS) {
	var zero T
	m[vfsBucket(v)][v.Rel] = zero
}

// AddIfAbsent inserts v and reports whether it was newly added.
func (m VFSMap[T]) AddIfAbsent(v VFS) bool {
	b := m[vfsBucket(v)]
	if _, ok := b[v.Rel]; ok {
		return false
	}

	var zero T
	b[v.Rel] = zero

	return true
}
