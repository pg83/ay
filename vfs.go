package main

import (
	"encoding/json"
)

type VFSRoot uint8

const (
	VFSRootSource VFSRoot = iota
	VFSRootBuild
)

type VFS uint32

const vfsPrefixLen = len("$(S)/")

func vfsBound() uint32 {
	return uint32(len(internTable.strs)) << 1
}

func intern(full string) VFS {
	root := VFSRootSource

	if full[2] == 'B' {
		root = VFSRootBuild
	}

	return VFS(uint32(internStr(full))<<1 | uint32(root))
}

func (v VFS) strID() uint32 {
	return uint32(v) >> 1
}

// str returns the STR backing this VFS's full path — a free conversion (VFS is
// STR<<1|root).
func (v VFS) str() STR {
	return STR(v.strID())
}

// vfsPrefixScratch backs internPrefixed's prefix+rel assembly, replacing a heap
// concat per call. Same single-writer contract as internTable: gen is single-
// threaded and executor goroutines must not intern.
var vfsPrefixScratch []byte

func internPrefixed(prefix, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)
	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internBytes(vfsPrefixScratch)
}

// internedPrefixed is the lookup-only twin of internPrefixed.
func internedPrefixed(prefix, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)
	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internedBytes(vfsPrefixScratch)
}

// internPrefixedJoined interns prefix+dir+"/"+rel (or prefix+rel when dir is
// empty) — the joined-path twin of internPrefixed.
func internPrefixedJoined(prefix, dir, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)

	if dir != "" {
		vfsPrefixScratch = append(vfsPrefixScratch, dir...)
		vfsPrefixScratch = append(vfsPrefixScratch, '/')
	}

	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internBytes(vfsPrefixScratch)
}

// internedPrefixedJoined is the lookup-only twin of internPrefixedJoined.
func internedPrefixedJoined(prefix, dir, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)

	if dir != "" {
		vfsPrefixScratch = append(vfsPrefixScratch, dir...)
		vfsPrefixScratch = append(vfsPrefixScratch, '/')
	}

	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internedBytes(vfsPrefixScratch)
}

// sourceJoined / buildJoined intern "$(S)/dir/rel" / "$(B)/dir/rel". rel must
// already be clean (pathIsClean).
func sourceJoined(dir, rel string) VFS {
	return VFS(uint32(internPrefixedJoined("$(S)/", dir, rel))<<1 | uint32(VFSRootSource))
}

func buildJoined(dir, rel string) VFS {
	return VFS(uint32(internPrefixedJoined("$(B)/", dir, rel))<<1 | uint32(VFSRootBuild))
}

// sourceBytes interns "$(S)/<rel>" from raw bytes — the parser-side twin of source().
func sourceBytes(rel []byte) VFS {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], "$(S)/"...)
	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return VFS(uint32(internBytes(vfsPrefixScratch))<<1 | uint32(VFSRootSource))
}

func source(rel string) VFS {
	return VFS(uint32(internPrefixed("$(S)/", rel))<<1 | uint32(VFSRootSource))
}

func build(rel string) VFS {
	return VFS(uint32(internPrefixed("$(B)/", rel))<<1 | uint32(VFSRootBuild))
}

// vfs converts a STR backing a rooted path ("$(S)/…" / "$(B)/…") into the bound
// VFS, no re-interning. Returns 0 for a non-rooted string.
func (id STR) vfs() VFS {
	s := internTable.strs[id]

	if !vfsHasPrefix(s) {
		return 0
	}

	root := VFSRootSource

	if s[2] == 'B' {
		root = VFSRootBuild
	}

	return VFS(uint32(id)<<1 | uint32(root))
}

func (v VFS) rel() string {
	return internTable.strs[v.strID()][vfsPrefixLen:]
}

// isSource / isBuild read the root bit directly (0=source, 1=build).
func (v VFS) isSource() bool {
	return uint32(v)&1 == 0
}

func (v VFS) isBuild() bool {
	return uint32(v)&1 != 0
}

func (v VFS) string() string {
	return internTable.strs[v.strID()]
}

// String implements fmt.Stringer.
func (v VFS) String() string {
	return v.string()
}

func (v VFS) longString() string {
	if v.isBuild() {
		return "$(BUILD_ROOT)/" + v.rel()
	}

	return "$(SOURCE_ROOT)/" + v.rel()
}

// vfsHasPrefix gates on "$(": every classified string is a rooted path, so the
// two-byte check suffices.
func vfsHasPrefix(s string) bool {
	return len(s) >= vfsPrefixLen && s[0] == '$' && s[1] == '('
}

func (v VFS) marshalJSON() ([]byte, error) {
	return json.Marshal(v.string())
}

// MarshalJSON implements json.Marshaler.
func (v VFS) MarshalJSON() ([]byte, error) {
	return v.marshalJSON()
}
