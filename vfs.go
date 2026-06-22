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

func (v VFS) str() STR {
	return STR(v.strID())
}

var vfsPrefixScratch []byte

func internPrefixed(prefix, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)
	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internBytes(vfsPrefixScratch)
}

func internedPrefixed(prefix, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)
	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internedBytes(vfsPrefixScratch)
}

func internPrefixedJoined(prefix, dir, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)

	if dir != "" {
		vfsPrefixScratch = append(vfsPrefixScratch, dir...)
		vfsPrefixScratch = append(vfsPrefixScratch, '/')
	}

	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internBytes(vfsPrefixScratch)
}

func internedPrefixedJoined(prefix, dir, rel string) STR {
	vfsPrefixScratch = append(vfsPrefixScratch[:0], prefix...)

	if dir != "" {
		vfsPrefixScratch = append(vfsPrefixScratch, dir...)
		vfsPrefixScratch = append(vfsPrefixScratch, '/')
	}

	vfsPrefixScratch = append(vfsPrefixScratch, rel...)

	return internedBytes(vfsPrefixScratch)
}

func sourceJoined(dir, rel string) VFS {
	return VFS(uint32(internPrefixedJoined("$(S)/", dir, rel))<<1 | uint32(VFSRootSource))
}

func buildJoined(dir, rel string) VFS {
	return VFS(uint32(internPrefixedJoined("$(B)/", dir, rel))<<1 | uint32(VFSRootBuild))
}

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

func (v VFS) isSource() bool {
	return uint32(v)&1 == 0
}

func (v VFS) isBuild() bool {
	return uint32(v)&1 != 0
}

func (v VFS) string() string {
	return internTable.strs[v.strID()]
}

func (v VFS) String() string {
	return v.string()
}

func (v VFS) longString() string {
	if v.isBuild() {
		return "$(BUILD_ROOT)/" + v.rel()
	}

	return "$(SOURCE_ROOT)/" + v.rel()
}

func vfsHasPrefix(s string) bool {
	return len(s) >= vfsPrefixLen && s[0] == '$' && s[1] == '('
}

func (v VFS) marshalJSON() ([]byte, error) {
	return json.Marshal(v.string())
}

func (v VFS) MarshalJSON() ([]byte, error) {
	return v.marshalJSON()
}
