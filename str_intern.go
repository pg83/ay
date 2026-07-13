package main

import (
	"unsafe"

	"github.com/zeebo/xxh3"
)

var internTable = struct {
	ids      *IntMap[STR]
	overflow map[string]STR
	flat     []InternCell
	cells    PageVec[InternCell]
	cellPage []InternCell
	cellBase uint32
	cellIdx  int
	count    uint32
	bytes    *BumpAllocator[byte]
}{
	ids:      newIntMap[STR](1 << 19),
	overflow: make(map[string]STR),
	flat:     make([]InternCell, 1, 1<<20),
	count:    1,
	bytes:    newBumpAllocator[byte](),
}

type InternCell struct {
	str string
	lo  uint64
}

func internOwnedCopy(b []byte) string {
	n := len(b)

	if n == 0 {
		return ""
	}

	block := internTable.bytes.alloc(n)

	copy(block, b)
	internTable.bytes.commit(n)

	return unsafe.String(&block[0], n)
}

type STR uint32

func (s STR) strID() uint32 {
	return uint32(s)
}

func strBound() uint32 {
	return internTable.count
}

func internAppend(s string, lo uint64) STR {
	id := STR(internTable.count)
	cell := InternCell{str: s, lo: lo}

	internTable.flat = append(internTable.flat, cell)

	if internTable.cellPage == nil {
		zeroPage := make([]InternCell, 1)
		page := make([]InternCell, 2)

		page[0] = cell
		internTable.cellPage = page
		internTable.cellBase = internTable.count
		internTable.cellIdx = 1
		internTable.cells.pages[0].Store(&zeroPage)
		internTable.cells.pages[1].Store(&page)
		internTable.count++

		return id
	}

	off := internTable.count - internTable.cellBase

	if int(off) == len(internTable.cellPage) {
		page := make([]InternCell, 2*len(internTable.cellPage))

		internTable.cellPage = page
		internTable.cellBase = internTable.count
		internTable.cellIdx++
		off = 0
		page[0] = cell
		internTable.cells.pages[internTable.cellIdx].Store(&page)
	} else {
		internTable.cellPage[off] = cell
	}

	internTable.count++

	return id
}

func internStr(s string) STR {
	h := xxh3.HashString128(s)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.flat[uint32(*p)].lo == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[s]; ok {
			return oid
		}

		owned := internOwnedCopy(unsafe.Slice(unsafe.StringData(s), len(s)))
		id := internAppend(owned, h.Lo)

		internTable.overflow[owned] = id

		return id
	}

	id := internAppend(internOwnedCopy(unsafe.Slice(unsafe.StringData(s), len(s))), h.Lo)

	internTable.ids.put(h.Hi, id)

	return id
}

func internFill(prefix string, parts []string) ([]byte, int) {
	switch len(parts) {
	case 0:
		n := len(prefix)
		block := internTable.bytes.alloc(n)

		copy(block, prefix)

		return block, n
	case 1:
		p0 := parts[0]
		n := len(prefix) + len(p0)
		block := internTable.bytes.alloc(n)
		off := copy(block, prefix)

		copy(block[off:], p0)

		return block, n
	case 2:
		p0, p1 := parts[0], parts[1]
		n := len(prefix) + len(p0) + len(p1)
		block := internTable.bytes.alloc(n)
		off := copy(block, prefix)
		off += copy(block[off:], p0)

		copy(block[off:], p1)

		return block, n
	case 3:
		p0, p1, p2 := parts[0], parts[1], parts[2]
		n := len(prefix) + len(p0) + len(p1) + len(p2)
		block := internTable.bytes.alloc(n)
		off := copy(block, prefix)
		off += copy(block[off:], p0)
		off += copy(block[off:], p1)

		copy(block[off:], p2)

		return block, n
	default:
		n := len(prefix)

		for _, p := range parts {
			n += len(p)
		}

		block := internTable.bytes.alloc(n)
		off := copy(block, prefix)

		for _, p := range parts {
			off += copy(block[off:], p)
		}

		return block, n
	}
}

func internBuild(prefix string, parts []string) STR {
	block, n := internFill(prefix, parts)

	if n == 0 {
		return strEmpty
	}

	return internBlock(block, n)
}

func internBuildBytes(prefix string, rel []byte) STR {
	n := len(prefix) + len(rel)
	block := internTable.bytes.alloc(n)
	off := copy(block, prefix)

	copy(block[off:], rel)

	return internBlock(block, n)
}

func internedBuild(prefix string, parts []string) STR {
	block, n := internFill(prefix, parts)

	return internedBytes(block[:n])
}

func internBlock(block []byte, n int) STR {
	buf := block[:n]
	h := xxh3.Hash128(buf)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.flat[uint32(*p)].lo == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(buf)]; ok {
			return oid
		}

		s := internCommitBlock(block, n)
		id := internAppend(s, h.Lo)

		internTable.overflow[s] = id

		return id
	}

	s := internCommitBlock(block, n)
	id := internAppend(s, h.Lo)

	internTable.ids.put(h.Hi, id)

	return id
}

func internCommitBlock(block []byte, n int) string {
	s := unsafe.String(&block[0], n)

	internTable.bytes.commit(n)

	return s
}

func internBytes(b []byte) STR {
	h := xxh3.Hash128(b)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.flat[uint32(*p)].lo == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(b)]; ok {
			return oid
		}

		s := internOwnedCopy(b)
		id := internAppend(s, h.Lo)

		internTable.overflow[s] = id

		return id
	}

	id := internAppend(internOwnedCopy(b), h.Lo)

	internTable.ids.put(h.Hi, id)

	return id
}

func internedBytes(b []byte) STR {
	h := xxh3.Hash128(b)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.flat[uint32(*p)].lo == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(b)]; ok {
			return oid
		}
	}

	return 0
}

func (id STR) str() STR {
	return id
}

func (id STR) string() string {
	return internTable.flat[uint32(id)].str
}

func (id STR) sharedString() string {
	return internTable.cells.get(uint32(id)).str
}

func (id STR) String() string {
	return id.string()
}

func internStrs(ss []string) []STR {
	if len(ss) == 0 {
		return nil
	}

	out := make([]STR, len(ss))

	for i, s := range ss {
		out[i] = internStr(s)
	}

	return out
}

func interned(s string) STR {
	h := xxh3.HashString128(s)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.flat[uint32(*p)].lo == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[s]; ok {
			return oid
		}
	}

	return 0
}

func internBound() uint32 {
	return internTable.count
}

func (id STR) source() VFS {
	return VFS(uint32(id)<<1 | uint32(VFSRootSource))
}

func (id STR) build() VFS {
	return VFS(uint32(id)<<1 | uint32(VFSRootBuild))
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

func internVInto(prefix string, parts []string) STR {
	return internBuild(prefix, parts)
}

func internedVInto(prefix string, parts []string) STR {
	return internedBuild(prefix, parts)
}

func internV(parts ...string) STR {
	if len(parts) == 1 {
		return internStr(parts[0])
	}

	return internVInto("", parts)
}

func internedV(parts ...string) STR {
	if len(parts) == 1 {
		return interned(parts[0])
	}

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

func (s STR) any() ANY {
	return ANY(uint32(s) << 1)
}
