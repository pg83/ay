package main

import (
	"encoding/json"
)

// vfs.go — typed VFS path as an interned id.
//
// A VFS value addresses a file in one of two virtual roots (SOURCE_ROOT /
// BUILD_ROOT) by its root-relative path. The value is a `uint32`: bit 31 is
// the BUILD_ROOT flag, bits 0..30 are the id of the root-relative path in a
// process-global string-intern table. The zero value (VFS(0)) is the unset
// sentinel — `String()`/`Rel()`/`Root()` treat it as VFSRootUnset, mirroring
// the old struct's zero value. Materialisation to "$(S)/<rel>" / "$(B)/<rel>"
// happens only at the serializer boundary (gjson_write.go); source-tree
// existence and reads route through the FS abstraction (fs.go) keyed on the
// bare rel, recovered via Rel().
//
// Replacing the old {Root, Rel string} struct with an id shrinks every VFS
// from 24 to 4 bytes, turns map keys into mapaccess_fast32, and makes equality
// an integer compare. Rel()/Root() recover the content via an O(1) slice
// index — the round-trip is byte-exact by construction (same string → same id
// → same backing bytes).

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

// VFS addresses a file by (root, root-relative path), encoded as an interned
// id. Bit 31 = BUILD_ROOT; bits 0..30 = rel-id into vfsTable. VFS(0) is unset.
type VFS uint32

const vfsBuildBit = uint32(1) << 31

// vfsNone is the unset VFS — the replacement for the old `VFS{}` zero literal.
const vfsNone = VFS(0)

// vfsTable is the process-global, append-only VFS rel-string intern table.
// Append-only and content-addressed (a given string always maps to the same
// id), so it is a referentially-transparent flyweight, not mutable shared
// state. VFS values are constructed only during the serial gen phase; the
// parallel dump path decodes JSON into string maps and never builds a VFS, so
// no synchronisation is required. Index 0 is reserved so VFS(0) stays unset.
var vfsTable = struct {
	ids     map[string]uint32
	strings []string
}{
	ids:     make(map[string]uint32, 1<<16),
	strings: make([]string, 1, 1<<16),
}

// vfsInternRel returns the stable id for rel, interning it on first sight.
func vfsInternRel(rel string) uint32 {
	if id, ok := vfsTable.ids[rel]; ok {
		return id
	}

	id := uint32(len(vfsTable.strings))
	if id >= vfsBuildBit {
		panic("vfs: exhausted 31-bit rel-id space")
	}

	vfsTable.ids[rel] = id
	vfsTable.strings = append(vfsTable.strings, rel)

	return id
}

// Source constructs a SOURCE_ROOT-rooted VFS path.
func Source(rel string) VFS { return VFS(vfsInternRel(rel)) }

// Build constructs a BUILD_ROOT-rooted VFS path.
func Build(rel string) VFS { return VFS(vfsInternRel(rel) | vfsBuildBit) }

// relID returns the bits 0..30 rel-id (root flag masked off).
func (v VFS) relID() uint32 { return uint32(v) &^ vfsBuildBit }

// Rel recovers the root-relative path. The unset zero value returns "",
// matching the old {Root, Rel string} struct's zero-value field read (only
// String() panicked on unset). A non-zero but out-of-range id signals
// corruption and panics.
func (v VFS) Rel() string {
	id := v.relID()
	if id == 0 {
		return ""
	}
	if id >= uint32(len(vfsTable.strings)) {
		panic("VFS.Rel: out-of-range VFS id")
	}

	return vfsTable.strings[id]
}

// Root reports which root v is anchored under (VFSRootUnset for the zero value).
func (v VFS) Root() VFSRoot {
	if v.relID() == 0 {
		return VFSRootUnset
	}

	if uint32(v)&vfsBuildBit != 0 {
		return VFSRootBuild
	}

	return VFSRootSource
}

// IsSource reports whether v is anchored under SOURCE_ROOT.
func (v VFS) IsSource() bool { return v.Root() == VFSRootSource }

// IsBuild reports whether v is anchored under BUILD_ROOT.
func (v VFS) IsBuild() bool { return v.Root() == VFSRootBuild }

// String materialises the canonical "$(S)/<rel>" or "$(B)/<rel>" form
// used at the serializer boundary. The scanner / FS access path keys on
// the bare rel via Rel() and never materialises.
//
// Panics on a zero-valued VFS. Construction MUST go through
// Source()/Build().
func (v VFS) String() string {
	switch v.Root() {
	case VFSRootSource:
		return "$(S)/" + v.Rel()
	case VFSRootBuild:
		return "$(B)/" + v.Rel()
	}
	panic("VFS.String: zero-valued VFS (missing Root)")
}

// LongString materialises the legacy raw-graph root spelling used by the
// upstream stats_uid preimage.
func (v VFS) LongString() string {
	switch v.Root() {
	case VFSRootSource:
		return "$(SOURCE_ROOT)/" + v.Rel()
	case VFSRootBuild:
		return "$(BUILD_ROOT)/" + v.Rel()
	}
	panic("VFS.LongString: zero-valued VFS (missing Root)")
}

// ParseVFS recognises s as a "$(S)/..." or "$(B)/..."
// string and returns the corresponding VFS. Returns (vfsNone, false) when
// s lacks both recognised prefixes — callers handling such tokens
// (e.g. compound CmdArg substrings) keep them as strings.
func ParseVFS(s string) (VFS, bool) {
	if rel, ok := trimVFSPrefix(s, "$(S)/"); ok {
		return Source(rel), true
	}
	if rel, ok := trimVFSPrefix(s, "$(B)/"); ok {
		return Build(rel), true
	}
	return vfsNone, false
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
// renders VFS as its canonical string form rather than the bare integer.
// Production output goes through `gjson_write.go::appendVFS` which bypasses
// encoding/json entirely.
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
		out[i] = v.Rel()
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

// lessVFS orders VFS the way `a.String() < b.String()` would, byte-for-byte:
// `$(B)/<rel>` sorts before `$(S)/<rel>` (B < S), and within the same Root the
// trailing Rel orders lexicographically. Interned-id order is unrelated to
// lexical order, so this resolves the rel strings rather than comparing ids.
func lessVFS(a, b VFS) bool {
	ra, rb := a.Root(), b.Root()
	if ra != rb {
		return ra == VFSRootBuild
	}
	return a.Rel() < b.Rel()
}

// VFSMap is a map keyed by VFS. With VFS now an interned uint32 it routes
// through Go's mapaccess_fast32, faster than both the prior struct-keyed
// map[VFS]T (11.9 ns/op) and the two-bucket faststr workaround it replaced
// (9.1 ns/op). Kept as a named type with the same method API so existing call
// sites are unchanged.
type VFSMap[T any] map[VFS]T

// NewVFSMap constructs a VFSMap pre-sized to `capacity`.
func NewVFSMap[T any](capacity int) VFSMap[T] {
	return make(VFSMap[T], capacity)
}

// Get returns the value stored under v and a presence flag.
func (m VFSMap[T]) Get(v VFS) (T, bool) {
	val, ok := m[v]
	return val, ok
}

// Set stores val under v.
func (m VFSMap[T]) Set(v VFS, val T) { m[v] = val }

// Has reports presence.
func (m VFSMap[T]) Has(v VFS) bool {
	_, ok := m[v]
	return ok
}

// Delete removes v's entry (no-op when absent).
func (m VFSMap[T]) Delete(v VFS) { delete(m, v) }

// Len returns the entry count.
func (m VFSMap[T]) Len() int { return len(m) }

// Clear drops every entry, retaining the underlying allocation for reuse.
func (m VFSMap[T]) Clear() { clear(m) }

// VFSSet is a presence-only specialisation of VFSMap with a
// zero-byte value type. Common for DFS visited-sets.
type VFSSet = VFSMap[struct{}]

// NewVFSSet constructs a VFSSet pre-sized to `capacity`.
func NewVFSSet(capacity int) VFSSet {
	return make(VFSSet, capacity)
}

// Add inserts v into the set.
func (m VFSMap[T]) Add(v VFS) {
	var zero T
	m[v] = zero
}

// AddIfAbsent inserts v and reports whether it was newly added.
func (m VFSMap[T]) AddIfAbsent(v VFS) bool {
	if _, ok := m[v]; ok {
		return false
	}

	var zero T
	m[v] = zero

	return true
}
