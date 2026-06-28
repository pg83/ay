package main

import (
	"encoding/json"
)

const vfsPrefixLen = len("$(S)/")

const (
	VFSRootSource VFSRoot = iota
	VFSRootBuild
)

type VFSRoot uint8

type VFS uint32

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

func vfsScratchConcat(prefix string, parts []string) []byte {
	b := append(vfsPrefixScratch[:0], prefix...)

	for _, p := range parts {
		b = append(b, p...)
	}

	vfsPrefixScratch = b

	return b
}

func internVInto(prefix string, parts []string) STR {
	return internBytes(vfsScratchConcat(prefix, parts))
}

func internedVInto(prefix string, parts []string) STR {
	return internedBytes(vfsScratchConcat(prefix, parts))
}

func internV(parts ...string) STR {
	return internVInto("", parts)
}

func internedV(parts ...string) STR {
	return internedVInto("", parts)
}

func internPrefixed(prefix, rel string) STR {
	return internVInto(prefix, []string{rel})
}

func internedPrefixed(prefix, rel string) STR {
	return internedVInto(prefix, []string{rel})
}

func internPrefixedJoined(prefix, dir, rel string) STR {
	if dir == "" {
		return internVInto(prefix, []string{rel})
	}

	return internVInto(prefix, []string{dir, "/", rel})
}

func internedPrefixedJoined(prefix, dir, rel string) STR {
	if dir == "" {
		return internedVInto(prefix, []string{rel})
	}

	return internedVInto(prefix, []string{dir, "/", rel})
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

func source(parts ...string) VFS {
	return VFS(uint32(internVInto("$(S)/", parts))<<1 | uint32(VFSRootSource))
}

func build(parts ...string) VFS {
	return VFS(uint32(internVInto("$(B)/", parts))<<1 | uint32(VFSRootBuild))
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
