package main

import (
	"encoding/json"
)

var (
	srcRootDirVFS = source("")
	bldRootDirVFS = build("")
)

const vfsPrefixLen = len("$(S)/")

const (
	VFSRootSource VFSRoot = iota
	VFSRootBuild
)

type VFSRoot uint8

type VFS uint32

func vfsBound() uint32 {
	return internTable.count << 1
}

func intern(full string) VFS {
	root := VFSRootSource

	if full[2] == 'B' {
		root = VFSRootBuild
	}

	return VFS(uint32(internStr(full[vfsPrefixLen:]))<<1 | uint32(root))
}

func (v VFS) strID() uint32 {
	return uint32(v)
}

func (v VFS) rel() STR {
	return STR(uint32(v) >> 1)
}

func (v VFS) relString() string {
	return internCell(v.rel()).str
}

func (v VFS) sharedRel() string {
	return internTable.cells.get(uint32(v) >> 1).str
}

func (v VFS) prefix() string {
	if v.isBuild() {
		return "$(B)/"
	}

	return "$(S)/"
}

func sourceJoined(dir, rel string) VFS {
	return internJoined(dir, rel).source()
}

func buildJoined(dir, rel string) VFS {
	return internJoined(dir, rel).build()
}

func sourceBytes(rel []byte) VFS {
	return internBytes(rel).source()
}

func source(parts ...string) VFS {
	return internV(parts...).source()
}

func build(parts ...string) VFS {
	return internV(parts...).build()
}

func (v VFS) isSource() bool {
	return uint32(v)&1 == 0
}

func (v VFS) isBuild() bool {
	return uint32(v)&1 != 0
}

func cwdVFS(s string) VFS {
	switch {
	case s == "$(B)":
		return bldRootDirVFS
	case s == "$(S)":
		return srcRootDirVFS
	case vfsHasPrefix(s):
		return intern(s)
	}

	throwFmt("cwdVFS: unexpected cwd %q", s)

	return 0
}

func (v VFS) string() string {
	rel := v.relString()

	if rel == "" {
		return v.prefix()[:vfsPrefixLen-1]
	}

	return v.prefix() + rel
}

func (v VFS) sharedString() string {
	rel := v.sharedRel()

	if rel == "" {
		return v.prefix()[:vfsPrefixLen-1]
	}

	return v.prefix() + rel
}

func (v VFS) String() string {
	return v.string()
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

func (v VFS) any() ANY {
	return ANY(uint32(v)<<1 | 1)
}
