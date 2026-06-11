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

func Intern(full string) VFS {
	root := VFSRootSource

	if full[2] == 'B' {
		root = VFSRootBuild
	}

	return VFS(uint32(internStr(full))<<1 | uint32(root))
}

func (v VFS) strID() uint32 {
	return uint32(v) >> 1
}

// str returns the STR backing this VFS's full path — a free conversion (the VFS
// is STR<<1|root), uniform with ARG/ENV/TOK str() for cmd-arg assembly.
func (v VFS) str() STR {
	return STR(v.strID())
}

// vfsPrefixScratch backs internPrefixed's prefix+rel assembly, replacing a
// heap-allocated concat per call — on the (overwhelmingly common) intern hit
// that string was thrown away immediately, and on a miss internBytes makes the
// one stable copy anyway. Same single-writer contract as internTable and
// deduper: gen runs single-threaded, and executor goroutines must not intern
// (see the restoreInto comment in make.go).
var vfsPrefixScratch []byte

func internPrefixed(prefix, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)
	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internBytes(vfsPrefixScratch)
}

func Source(rel string) VFS {
	return VFS(uint32(internPrefixed("$(S)/", rel))<<1 | uint32(VFSRootSource))
}

func Build(rel string) VFS {
	return VFS(uint32(internPrefixed("$(B)/", rel))<<1 | uint32(VFSRootBuild))
}

// vfs converts a STR backing a full rooted path ("$(S)/…" / "$(B)/…") into the
// VFS bound to it — a shift plus the root bit, no re-interning (a VFS is
// STR<<1|root over the same table slot). Returns 0 for a non-rooted string.
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

func (v VFS) root() VFSRoot {
	if uint32(v)&1 != 0 {
		return VFSRootBuild
	}

	return VFSRootSource
}

func (v VFS) isSource() bool {
	return v.root() == VFSRootSource
}

func (v VFS) isBuild() bool {
	return v.root() == VFSRootBuild
}

func (v VFS) String() string {
	return internTable.strs[v.strID()]
}

func (v VFS) longString() string {
	if v.root() == VFSRootBuild {
		return "$(BUILD_ROOT)/" + v.rel()
	}

	return "$(SOURCE_ROOT)/" + v.rel()
}

// vfsHasPrefix gates on "$(": every "$( "-prefixed string we ever classify is a
// rooted "$(S)/…" / "$(B)/…" path, so the two-byte check suffices.
func vfsHasPrefix(s string) bool {
	return len(s) >= vfsPrefixLen && s[0] == '$' && s[1] == '('
}

func (v VFS) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}
