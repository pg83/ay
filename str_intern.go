package main

import (
	"unsafe"

	"github.com/zeebo/xxh3"
)

var internTable = newInternTable()

type InternTable struct {
	ids      *IntMap[STR]
	overflow map[string]STR
	bytes    ReservedArena
	pos      ReservedArena
	lo       ReservedArena
	count    uint32
	bound    uint32
	mapped   uint32

	cleanSeen BitSet
	cleanYes  BitSet

	frozen     *MmapedIntMap[STR]
	constCount uint32
	constBytes int
	imageCount uint32
}

func (t *InternTable) sealConstants() {
	t.constCount = t.count
	t.constBytes = t.bytes.used
}

func internFind(hi uint64) *STR {
	if frozen := internTable.frozen; frozen != nil {
		if p := frozen.get(hi); p != nil {
			return p
		}
	}

	return internTable.ids.get(hi)
}

func newInternTable() InternTable {
	t := InternTable{
		ids:      newIntMap[STR](1 << 19),
		overflow: make(map[string]STR),
		bytes:    reserveArena(strTextReserve),
		pos:      reserveArena(strIDReserve * 8),
		lo:       reserveArena(strIDReserve * 8),
		count:    1,
	}

	t.mapIDs(0)

	return t
}

func (t *InternTable) mapIDs(id uint32) {
	need := (int(id) + 1) * 8

	t.pos.grow(need)
	t.lo.grow(need)
	t.mapped = uint32(min(t.pos.mapped, t.lo.mapped) / 8)
}

func internString(id STR) string {
	e := *(*uint64)(unsafe.Add(internTable.pos.base, uintptr(id)<<3))

	return unsafe.String((*byte)(unsafe.Add(internTable.bytes.base, e&strOffMask)), e>>strOffBits)
}

func internLo(id STR) uint64 {
	return *(*uint64)(unsafe.Add(internTable.lo.base, uintptr(id)<<3))
}

func internOwnedCopy(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	return unsafe.String((*byte)(unsafe.Add(internTable.bytes.base, internStore(bytesString(b)))), len(b))
}

func internStore(s string) int {
	n := len(s)
	off := internTable.bytes.used

	copy(internTable.bytes.alloc(n), s)
	internTable.bytes.commit(n)

	return off
}

type STR uint32

func (s STR) strID() uint32 {
	return uint32(s)
}

func strBound() uint32 {
	if chaosInternShuffle {
		return internTable.bound
	}

	return internTable.count
}

func internSet(id uint32, off, n int, lo uint64) {
	if n > strLenMax {
		throwFmt("interned string of %d bytes is longer than %d", n, strLenMax)
	}

	if id >= internTable.mapped {
		internTable.mapIDs(id)
	}

	*(*uint64)(unsafe.Add(internTable.pos.base, uintptr(id)<<3)) = uint64(off) | uint64(n)<<strOffBits
	*(*uint64)(unsafe.Add(internTable.lo.base, uintptr(id)<<3)) = lo
}

func internAppend(off, n int, lo uint64) STR {
	id := internTable.count

	if chaosInternShuffle {
		id = chaosInternID(id)

		if id >= internTable.bound {
			internTable.bound = id + 1
		}
	}

	internSet(id, off, n, lo)

	internTable.count++

	return STR(id)
}

func internStr(s string) STR {
	h := chaosInternHash(xxh3.HashString128(s))

	if p := internFind(h.Hi); p != nil {
		if internLo(*p) == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[s]; ok {
			return oid
		}

		id := internAppend(internStore(s), len(s), h.Lo)

		internTable.overflow[id.string()] = id

		return id
	}

	id := internAppend(internStore(s), len(s), h.Lo)

	internTable.ids.put(h.Hi, id)

	return id
}

func internFill(prefix string, parts []string) ([]byte, int) {
	switch len(parts) {
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

	return internBlock(block, n)
}

func internedBuild(prefix string, parts []string) STR {
	block, n := internFill(prefix, parts)

	return internedBytes(block[:n])
}

func internBlock(block []byte, n int) STR {
	buf := block[:n]
	h := chaosInternHash(xxh3.Hash128(buf))

	if p := internFind(h.Hi); p != nil {
		if internLo(*p) == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(buf)]; ok {
			return oid
		}

		id := internAppend(internCommitBlock(n), n, h.Lo)

		internTable.overflow[id.string()] = id

		return id
	}

	id := internAppend(internCommitBlock(n), n, h.Lo)

	internTable.ids.put(h.Hi, id)

	return id
}

func internCommitBlock(n int) int {
	off := internTable.bytes.used

	internTable.bytes.commit(n)

	return off
}

func internBytes(b []byte) STR {
	h := chaosInternHash(xxh3.Hash128(b))

	if p := internFind(h.Hi); p != nil {
		if internLo(*p) == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(b)]; ok {
			return oid
		}

		id := internAppend(internStore(bytesString(b)), len(b), h.Lo)

		internTable.overflow[id.string()] = id

		return id
	}

	id := internAppend(internStore(bytesString(b)), len(b), h.Lo)

	internTable.ids.put(h.Hi, id)

	return id
}

func internedBytes(b []byte) STR {
	h := chaosInternHash(xxh3.Hash128(b))

	if p := internFind(h.Hi); p != nil {
		if internLo(*p) == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[string(b)]; ok {
			return oid
		}
	}

	return 0
}

func (id STR) string() string {
	return internString(id)
}

func (id STR) sharedString() string {
	return internString(id)
}

func interned(s string) STR {
	h := chaosInternHash(xxh3.HashString128(s))

	if p := internFind(h.Hi); p != nil {
		if internLo(*p) == h.Lo {
			return *p
		}

		if oid, ok := internTable.overflow[s]; ok {
			return oid
		}
	}

	return 0
}

func (id STR) source() VFS {
	return VFS(uint32(id)<<1 | uint32(VFSRootSource))
}

func (id STR) build() VFS {
	return VFS(uint32(id)<<1 | uint32(VFSRootBuild))
}

func (id STR) vfs() VFS {
	s := internString(id)

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
	return internedVInto("", parts)
}

func internedPrefixed(prefix, rel string) STR {
	return internedVInto(prefix, []string{rel})
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

func (id STR) isEmpty() bool {
	return id == 0 || id == strEmpty
}

func (id STR) cleanPath() bool {
	t := &internTable

	if t.cleanSeen.has(uint32(id)) {
		return t.cleanYes.has(uint32(id))
	}

	s := id.string()
	clean := s != "" && pathIsClean(s)

	t.cleanSeen.add(uint32(id))

	if clean {
		t.cleanYes.add(uint32(id))
	}

	return clean
}

func (id STR) cleanRel() string {
	if id.cleanPath() {
		return id.string()
	}

	return cleanRel(id.string())
}
