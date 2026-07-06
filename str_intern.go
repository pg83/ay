package main

import (
	"unsafe"

	"github.com/zeebo/xxh3"
)

var internTable = struct {
	ids      *IntMap[STR]
	overflow map[string]STR
	flat     []internCell
	cells    PageVec[internCell]
	count    uint32
	bytes    *BumpAllocator[byte]
}{
	ids:      newIntMap[STR](1 << 16),
	overflow: make(map[string]STR),
	flat:     make([]internCell, 1, 1<<20),
	count:    1,
	bytes:    newBumpAllocator[byte](1 << 20),
}

type internCell struct {
	str string
	lo  uint64
}

func init() {
	internTable.cells.set(0, internCell{})
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
	cell := internCell{str: s, lo: lo}

	internTable.flat = append(internTable.flat, cell)
	internTable.cells.set(internTable.count, cell)
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

		id := internAppend(s, h.Lo)

		internTable.overflow[s] = id

		return id
	}

	id := internAppend(s, h.Lo)

	internTable.ids.put(h.Hi, id)

	return id
}

func internFill(prefix string, parts []string) ([]byte, int) {
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
