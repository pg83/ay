package main

import (
	"encoding/json"
	"unsafe"

	"github.com/zeebo/xxh3"
)

type VFSRoot uint8

const (
	VFSRootSource VFSRoot = iota
	VFSRootBuild
)

type VFS uint32

const vfsPrefixLen = len("$(S)/")

type STR uint32

var internTable = struct {
	ids    map[string]STR
	strs   []string
	hashes []uint64
}{
	ids:    make(map[string]STR, 1<<16),
	strs:   make([]string, 1, 1<<16),
	hashes: make([]uint64, 1, 1<<16),
}

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
func (id STR) String() string { return internTable.strs[id] }
func interned(s string) *STR {
	if id, ok := internTable.ids[s]; ok {
		return &id
	}

	return nil
}
func internBound() uint32 { return uint32(len(internTable.strs)) }
func vfsBound() uint32    { return uint32(len(internTable.strs)) << 1 }
func Intern(full string) VFS {
	root := VFSRootSource

	if full[2] == 'B' {
		root = VFSRootBuild
	}

	return VFS(uint32(internString(full))<<1 | uint32(root))
}
func (v VFS) strID() uint32 { return uint32(v) >> 1 }
func Source(rel string) VFS { return Intern("$(S)/" + rel) }
func Build(rel string) VFS  { return Intern("$(B)/" + rel) }
func (v VFS) Rel() string {
	return internTable.strs[v.strID()][vfsPrefixLen:]
}

func (v VFS) Root() VFSRoot {
	if uint32(v)&1 != 0 {
		return VFSRootBuild
	}

	return VFSRootSource
}
func (v VFS) IsSource() bool { return v.Root() == VFSRootSource }
func (v VFS) IsBuild() bool  { return v.Root() == VFSRootBuild }
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

func vfsRelsSlice(vs []VFS) []string {
	out := make([]string, len(vs))

	for i, v := range vs {
		out[i] = v.Rel()
	}

	return out
}

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

func lessVFS(a, b VFS) bool {
	ra, rb := a.Root(), b.Root()

	if ra != rb {
		return ra == VFSRootBuild
	}

	return a.Rel() < b.Rel()
}
