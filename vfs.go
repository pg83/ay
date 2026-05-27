package main

import (
	"encoding/json"
	"unsafe"

	"github.com/zeebo/xxh3"
)

// vfs.go — typed VFS path as an interned id.
//
// A VFS value addresses a file in one of two virtual roots (SOURCE_ROOT /
// BUILD_ROOT). The value packs an intern id and the root into a `uint32`:
// v = internId<<1 | root, where the low bit is the root (0=Source, 1=Build)
// and the intern id (high bits) points into a process-global table that stores
// the FULL canonical string ("$(S)/<rel>" / "$(B)/<rel>"). A VFS is always
// constructed via Intern() (or the Source()/Build() wrappers); there is no
// "none" VFS — an optional path is a *VFS, nil when absent. Root() is a single
// bit test with no memory access, while String() — by far the hottest allocator
// in the serializer (~40% of all gen allocations) — stays a zero-alloc read of
// the interned full string at strs[v>>1] (no per-call "$(X)/"+rel concat). The
// root sits in the LOW bit deliberately: VFS ids stay in [0, 2*internBound),
// dense enough to index the closure DFS scratch sets directly (a high root bit
// would scatter ids across the full uint32 range and blow up those arrays).
//
// Rel() recovers the bare relative path as an O(1) slice of the interned full
// string (strip the fixed 5-byte "$(S)/"/"$(B)/" prefix); Root() tests the low
// bit. The FS abstraction (fs.go) keys on the bare rel via Rel().
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
// STR is the stable, dense id of an interned string — the value internString
// returns and the key type for maps that want a string identity without
// holding the string. A VFS is a STR whose string is a canonical
// "$(S)/<rel>" / "$(B)/<rel>" path; both share this one table and id space.
type STR uint32

// hashes[id] is xxh3-64 of strs[id], computed once at intern time. The node
// canonicalizer writes this fixed-width content hash for a VFS instead of
// copying its (variable-length) path, so a node's UID preimage stays
// content-stable across runs (it is a hash of the string body, not the
// run-local id) while avoiding per-input memmove of the path bytes.
var internTable = struct {
	ids    map[string]STR
	strs   []string
	hashes []uint64
}{
	ids:    make(map[string]STR, 1<<16),
	strs:   make([]string, 1, 1<<16),
	hashes: make([]uint64, 1, 1<<16),
}

// internString returns the stable id for s, interning it on first sight.
func internString(s string) STR {
	if id, ok := internTable.ids[s]; ok {
		return id
	}

	id := STR(len(internTable.strs))
	internTable.ids[s] = id
	internTable.strs = append(internTable.strs, s)
	internTable.hashes = append(internTable.hashes, xxh3.HashString(s))

	return id
}

// internBytes is internString from a byte slice without allocating a string for
// the (common) hit: the lookup uses a non-retaining unsafe string view of b, and
// only a first-sight miss copies. Used by the include parsers, where the same
// target bytes recur across thousands of files.
func internBytes(b []byte) STR {
	if id, ok := internTable.ids[unsafe.String(unsafe.SliceData(b), len(b))]; ok {
		return id
	}

	s := string(b)
	id := STR(len(internTable.strs))
	internTable.ids[s] = id
	internTable.strs = append(internTable.strs, s)
	internTable.hashes = append(internTable.hashes, xxh3.Hash(b))

	return id
}

// String returns the interned string for id (a zero-alloc table read).
func (id STR) String() string { return internTable.strs[id] }

// interned returns s's id if it has already been interned, else nil — a
// read-only probe that never mutates the table. Lets a caller ask "is this
// string known?" (and key an id-map by the result) without polluting the
// append-only table with a one-shot lookup string.
func interned(s string) *STR {
	if id, ok := internTable.ids[s]; ok {
		return &id
	}

	return nil
}

// internBound is an exclusive upper bound on interned STR ids — every live id
// is in [1, internBound()). Used to size STR-indexed scratch (e.g. the sysincl
// could-claim bitset).
func internBound() uint32 { return uint32(len(internTable.strs)) }

// vfsBound is an exclusive upper bound on VFS ids. A VFS packs the intern id in
// the high bits and the root in the low bit (v = internId<<1 | root), so VFS
// ids reach 2*internBound. Used to size VFS-id-indexed scratch (the closure
// DFS visited / Tarjan sets).
func vfsBound() uint32 { return uint32(len(internTable.strs)) << 1 }

// Intern returns the VFS for the full canonical "$(S)/<rel>" / "$(B)/<rel>"
// string. A literal call — Intern("$(S)/build/scripts/x.py") — keys the lookup
// on a compile-time constant with no per-call concat; Source()/Build() are the
// wrappers for a runtime rel. The precondition for a token of unknown shape is
// vfsHasPrefix. The root (byte 2, 'S'/'B') is packed into the low bit so Root()
// needs no table read.
func Intern(full string) VFS {
	root := VFSRootSource
	if full[2] == 'B' {
		root = VFSRootBuild
	}

	return VFS(uint32(internString(full))<<1 | uint32(root))
}

// strID is the intern id of v's full canonical string (the high bits; the low
// bit is the root). v.String() == strID().String().
func (v VFS) strID() uint32 { return uint32(v) >> 1 }

// Source constructs a SOURCE_ROOT-rooted VFS from a runtime rel.
func Source(rel string) VFS { return Intern("$(S)/" + rel) }

// Build constructs a BUILD_ROOT-rooted VFS from a runtime rel.
func Build(rel string) VFS { return Intern("$(B)/" + rel) }

// Rel recovers the root-relative path as an O(1) slice of the interned full
// string (strip the 5-byte "$(S)/"/"$(B)/" prefix).
func (v VFS) Rel() string {
	return internTable.strs[v.strID()][vfsPrefixLen:]
}

// Root reports which root v is anchored under — a test of the low bit, which
// Intern set from the canonical "$(S)/" / "$(B)/" prefix.
func (v VFS) Root() VFSRoot {
	if uint32(v)&1 != 0 {
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
	return internTable.strs[v.strID()]
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
