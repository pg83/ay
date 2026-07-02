package main

import (
	"unsafe"

	"github.com/zeebo/xxh3"
)

var internTable = struct {
	ids      *IntMap[STR]
	overflow map[string]STR
	los      []uint64
	strs     []string

	bytes *BumpAllocator[byte]
}{
	ids:      newIntMap[STR](1 << 16),
	overflow: make(map[string]STR),
	los:      make([]uint64, 1, 1<<16),
	strs:     make([]string, 1, 1<<16),
	bytes:    newBumpAllocator[byte](1 << 20),
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

func internAppend(s string, lo uint64) STR {
	id := STR(len(internTable.strs))

	internTable.strs = append(internTable.strs, s)
	internTable.los = append(internTable.los, lo)

	return id
}

func internStr(s string) STR {
	h := xxh3.HashString128(s)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
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

func internBuild(prefix string, parts []string) STR {
	n := len(prefix)

	for _, p := range parts {
		n += len(p)
	}

	if n == 0 {
		return internStr("")
	}

	block := internTable.bytes.alloc(n)
	off := copy(block, prefix)

	for _, p := range parts {
		off += copy(block[off:], p)
	}

	buf := block[:n]
	h := xxh3.Hash128(buf)

	if p := internTable.ids.get(h.Hi); p != nil {
		if internTable.los[*p] == h.Lo {
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
		if internTable.los[*p] == h.Lo {
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
		if internTable.los[*p] == h.Lo {
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
	if strProbeEnabled {
		strProbeAt()
	}

	return internTable.strs[id]
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
		if internTable.los[*p] == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[s]; ok {
			return oid
		}
	}

	return 0
}

func internBound() uint32 {
	return uint32(len(internTable.strs))
}
