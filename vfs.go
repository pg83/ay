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

func (v VFS) Rel() string {
	return internTable.strs[v.strID()][vfsPrefixLen:]
}

func (v VFS) Root() VFSRoot {
	if uint32(v)&1 != 0 {
		return VFSRootBuild
	}

	return VFSRootSource
}

func (v VFS) IsSource() bool {
	return v.Root() == VFSRootSource
}

func (v VFS) IsBuild() bool {
	return v.Root() == VFSRootBuild
}

func (v VFS) String() string {
	return internTable.strs[v.strID()]
}

func (v VFS) LongString() string {
	if v.Root() == VFSRootBuild {
		return "$(BUILD_ROOT)/" + v.Rel()
	}

	return "$(SOURCE_ROOT)/" + v.Rel()
}

func vfsHasPrefix(s string) bool {
	return len(s) >= vfsPrefixLen && (s[:vfsPrefixLen] == "$(S)/" || s[:vfsPrefixLen] == "$(B)/")
}

func (v VFS) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}
