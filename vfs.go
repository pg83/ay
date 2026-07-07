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

var vfsFull DenseMap[VFS, STR]

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
	return internTable.flat[uint32(v)>>1].str
}

func (v VFS) sharedRel() string {
	return internTable.cells.get(uint32(v) >> 1).str
}

func (id STR) source() VFS {
	return VFS(uint32(id)<<1 | uint32(VFSRootSource))
}

func (id STR) build() VFS {
	return VFS(uint32(id)<<1 | uint32(VFSRootBuild))
}

func (v VFS) prefix() string {
	if v.isBuild() {
		return "$(B)/"
	}

	return "$(S)/"
}

func internVInto(prefix string, parts []string) STR {
	return internBuild(prefix, parts)
}

func internedVInto(prefix string, parts []string) STR {
	return internedBuild(prefix, parts)
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

func internJoined(dir, rel string) STR {
	if dir == "" {
		return internStr(rel)
	}

	return internV(dir, "/", rel)
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

func (id STR) vfs() VFS {
	s := internTable.flat[uint32(id)].str

	if !vfsHasPrefix(s) {
		return 0
	}

	root := VFSRootSource

	if s[2] == 'B' {
		root = VFSRootBuild
	}

	return VFS(uint32(internStr(s[vfsPrefixLen:]))<<1 | uint32(root))
}

func (v VFS) isSource() bool {
	return uint32(v)&1 == 0
}

func (v VFS) isBuild() bool {
	return uint32(v)&1 != 0
}

var (
	srcRootDirVFS = source("")
	bldRootDirVFS = build("")
)

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

func (v VFS) fullSTR() STR {
	if s, ok := vfsFull.get(v); ok {
		return s
	}

	rel := v.relString()
	s := internPrefixed(v.prefix(), rel)

	if rel == "" {
		if v.isBuild() {
			s = strB
		} else {
			s = strS
		}
	}

	vfsFull.put(v, s)

	return s
}

func (v VFS) string() string {
	return v.fullSTR().string()
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

func (v VFS) longString() string {
	if v.isBuild() {
		return "$(BUILD_ROOT)/" + v.relString()
	}

	return "$(SOURCE_ROOT)/" + v.relString()
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
