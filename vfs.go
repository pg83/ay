package main

import "encoding/json"

// vfs.go — typed VFS path.
//
// A `VFS` value addresses a file in one of two virtual roots — SOURCE_ROOT
// (the source tree) or BUILD_ROOT (the build-output tree) — by its
// root-relative path. The previous codebase carried these as plain
// `"$(SOURCE_ROOT)/<rel>"` / `"$(BUILD_ROOT)/<rel>"` strings, which forced
// a string concat at every construction site (4.7M allocations per M3
// run, profiled as the #1 alloc hotspot) and lost type information at
// the boundary between scanner / emitter / serializer.
//
// VFS is a comparable struct, so it works as a map key and as a struct
// field. Materialisation to the on-disk JSON string happens only at
// the serializer boundary (`gjson_write.go`) and at the os.Stat
// boundary inside the scanner.

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

// String materialises the canonical "$(SOURCE_ROOT)/<rel>" or
// "$(BUILD_ROOT)/<rel>" form. Used at the serializer boundary; the
// scanner os.Stat path bypasses it and concatenates `sourceRoot + rel`
// directly to avoid two materialisations.
//
// Panics on a zero-valued VFS. Construction MUST go through
// Source()/Build() (or struct-literal with an explicit Root).
func (v VFS) String() string {
	switch v.Root {
	case VFSRootSource:
		return vfsSourcePrefix + v.Rel
	case VFSRootBuild:
		return vfsBuildPrefix + v.Rel
	}
	panic("VFS.String: zero-valued VFS (missing Root)")
}

// ParseVFS recognises s as a "$(SOURCE_ROOT)/..." or "$(BUILD_ROOT)/..."
// string and returns the corresponding VFS. Returns (zero, false) when
// s lacks both recognised prefixes — callers handling such tokens
// (e.g. compound CmdArg substrings) keep them as strings.
func ParseVFS(s string) (VFS, bool) {
	if rel, ok := trimVFSPrefix(s, vfsSourcePrefix); ok {
		return Source(rel), true
	}
	if rel, ok := trimVFSPrefix(s, vfsBuildPrefix); ok {
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

// ParseVFSOrSource parses s as a VFS path if it carries a recognised
// "$(SOURCE_ROOT)/" or "$(BUILD_ROOT)/" prefix; otherwise it returns
// Source(s) verbatim. Used by `ToVFSSlice` for the migration period —
// synthetic emitter tests use bare relative strings (e.g. "c.in") that
// don't carry a prefix; treating those as SOURCE_ROOT-rooted keeps the
// test fixtures working without forcing every test literal to be
// rewritten.
func ParseVFSOrSource(s string) VFS {
	if v, ok := ParseVFS(s); ok {
		return v
	}
	return Source(s)
}

// VFSesFromStrings is the bulk variant of ParseVFSOrSource used by
// scanner-result conversion sites.
func VFSesFromStrings(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}
	return out
}

// vfsStringsSlice materialises a []VFS as a []string of canonical VFS
// strings. Used at boundaries where downstream APIs still take
// []string (memberInputs aggregator, AR input bucket, etc.).
func vfsStringsSlice(vs []VFS) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.String()
	}
	return out
}

// ToVFSSlice converts a []string (each already in VFS form or a bare
// rel) to []VFS. Migration shim: emitter sites currently assemble
// Inputs / Outputs as []string; wrapping the result here keeps the
// build green while each site is independently rewritten to construct
// VFS values directly (which is where the alloc-reduction win lives).
//
// Returns `[]VFS{}` (non-nil, length 0) for an empty input so the
// downstream JSON serializer emits `[]` rather than `null` — the
// reference graph and the production writer both use the non-nil
// empty form.
func ToVFSSlice(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}
	return out
}

// VFSMap is a two-bucket map keyed by VFS that dispatches on `Root`
// (Source → bucket 0, Build → bucket 1) and stores values under the
// rel-string portion. Compared to `map[VFS]T`, lookups go through
// Go's `mapaccess2_faststr` fast path (specialised for `map[string]T`)
// instead of the generic struct-keyed path which calls
// `type:.hash.main.VFS` via an indirect type descriptor and pays a
// ~2.5–2.8× per-op penalty in the scanner DFS hot loop (verified
// against PR-M3-vfs-deep regression profiles, `walkSubgraph.func1`:
// 373ms faststr → 1047ms generic for the same workload).
//
// Generic Go specialises one map per instantiation; the bench
// (`BenchmarkMapAccess_VFS2Bucket_Generic` vs
// `BenchmarkMapAccess_VFS2Bucket_NonGeneric`) shows 0% overhead vs
// hand-rolled, and 9.1 ns/op beats both `map[VFS]T` (11.9 ns/op) and
// `map[string]T` keyed by the materialised "$(ROOT)/<rel>" form
// (10.6 ns/op).
//
// The two buckets are exposed as a [2]-array so callers can range
// them deterministically when needed.
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
