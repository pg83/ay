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

func Source(rel string) VFS {
	return Intern("$(S)/" + rel)
}

func Build(rel string) VFS {
	return Intern("$(B)/" + rel)
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
