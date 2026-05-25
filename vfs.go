package main

import (
	"encoding/json"
)

// vfs.go — typed VFS path as an interned id.
//
// A VFS value addresses a file in one of two virtual roots (SOURCE_ROOT /
// BUILD_ROOT). The value is a plain `uint32` id into a process-global intern
// table that stores the FULL canonical string ("$(S)/<rel>" / "$(B)/<rel>").
// A VFS is always constructed via Intern() (or the Source()/Build() wrappers);
// there is no "none" VFS — an optional path is a *VFS, nil when absent. Because
// the root is baked into the interned string there is no root bit in the id,
// and String() — by far the hottest allocator in the serializer (~40% of all
// gen allocations) — is a zero-alloc table read instead of a per-call
// "$(X)/"+rel concatenation.
//
// Rel() recovers the bare relative path as an O(1) slice of the interned full
// string (strip the fixed 5-byte "$(S)/"/"$(B)/" prefix); Root() reads one
// byte. The FS abstraction (fs.go) keys on the bare rel via Rel().
//
// The intern map is keyed by the full string. A call with a literal token,
// Intern("$(S)/<lit>"), keys the lookup on a compile-time constant with no
// concat; Source()/Build() prepend the prefix for a runtime rel. VFS values are
// constructed only during the serial gen phase (the parallel dump path decodes
// JSON into string maps and never builds a VFS), so the table needs no
// synchronisation. Map keys turn into mapaccess_fast32 and equality is an
// integer compare.

// VFSRoot identifies which root a `VFS` is anchored under. A VFS is always
// one of the two — there is no unset root (optionality is modelled with *VFS).
type VFSRoot uint8

const (
	VFSRootSource VFSRoot = iota
	VFSRootBuild
)

// VFS addresses a file by (root, root-relative path), encoded as a plain
// interned id into internTable.strs. It is always constructed via Intern() (or the
// Source()/Build() wrappers); an optional VFS is a *VFS, nil when absent —
// there is no in-band "none" VFS value.
type VFS uint32

// vfsPrefixLen is the length of the "$(S)/" / "$(B)/" canonical root prefix;
// the discriminating byte ('S'/'B') sits at index 2.
const vfsPrefixLen = len("$(S)/")

// internTable is the process-global, append-only string-intern table:
// internString maps any string to a stable dense uint32 id. A VFS is the id
// of a canonical "$(S)/<rel>" / "$(B)/<rel>" string; the include scanner reuses
// the SAME table for compact cache keys (raw `#include` targets / includer
// rels), so a string is hashed once and reused as a uint32 key everywhere.
// Append-only and content-addressed (a given string always maps to the same
// id), so it is a referentially-transparent flyweight, not mutable shared
// state. Interning happens only during the serial gen phase (the parallel dump
// path decodes JSON into string maps and never interns), so no synchronisation
// is required. Index 0 is reserved (strs[0] == "") so the zero id is never a
// real string.
var internTable = struct {
	ids  map[string]uint32
	strs []string
}{
	ids:  make(map[string]uint32, 1<<16),
	strs: make([]string, 1, 1<<16),
}

// internString returns the stable id for s, interning it on first sight.
func internString(s string) uint32 {
	if id, ok := internTable.ids[s]; ok {
		return id
	}

	id := uint32(len(internTable.strs))
	internTable.ids[s] = id
	internTable.strs = append(internTable.strs, s)

	return id
}

// internBound is an exclusive upper bound on interned ids — every live id is
// in [1, internBound()). Used to size id-indexed scratch sets (the scanner's
// DFS visited set).
func internBound() uint32 { return uint32(len(internTable.strs)) }

// Intern returns the VFS for the full canonical "$(S)/<rel>" / "$(B)/<rel>"
// string. A literal call — Intern("$(S)/build/scripts/x.py") — keys the lookup
// on a compile-time constant with no per-call concat; Source()/Build() are the
// wrappers for a runtime rel. The precondition for a token of unknown shape is
// vfsHasPrefix.
func Intern(full string) VFS { return VFS(internString(full)) }

// Source constructs a SOURCE_ROOT-rooted VFS from a runtime rel.
func Source(rel string) VFS { return Intern("$(S)/" + rel) }

// Build constructs a BUILD_ROOT-rooted VFS from a runtime rel.
func Build(rel string) VFS { return Intern("$(B)/" + rel) }

// Rel recovers the root-relative path as an O(1) slice of the interned full
// string (strip the 5-byte "$(S)/"/"$(B)/" prefix).
func (v VFS) Rel() string {
	return internTable.strs[uint32(v)][vfsPrefixLen:]
}

// Root reports which root v is anchored under. The canonical prefix is "$(S)/"
// or "$(B)/"; byte 2 ('S'/'B') discriminates.
func (v VFS) Root() VFSRoot {
	if internTable.strs[uint32(v)][2] == 'B' {
		return VFSRootBuild
	}

	return VFSRootSource
}

// IsSource reports whether v is anchored under SOURCE_ROOT.
func (v VFS) IsSource() bool { return v.Root() == VFSRootSource }

// IsBuild reports whether v is anchored under BUILD_ROOT.
func (v VFS) IsBuild() bool { return v.Root() == VFSRootBuild }

// String returns the canonical "$(S)/<rel>" / "$(B)/<rel>" form — a direct
// read of the interned full string, no allocation. The scanner / FS access
// path keys on the bare rel via Rel() and never materialises.
func (v VFS) String() string {
	return internTable.strs[uint32(v)]
}

// LongString materialises the legacy raw-graph root spelling used by the
// upstream stats_uid preimage.
func (v VFS) LongString() string {
	if v.Root() == VFSRootBuild {
		return "$(BUILD_ROOT)/" + v.Rel()
	}

	return "$(SOURCE_ROOT)/" + v.Rel()
}

// vfsHasPrefix reports whether s carries a canonical "$(S)/" / "$(B)/" root
// prefix and is therefore directly Intern-able as a full token.
func vfsHasPrefix(s string) bool {
	return len(s) >= vfsPrefixLen && (s[:vfsPrefixLen] == "$(S)/" || s[:vfsPrefixLen] == "$(B)/")
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
