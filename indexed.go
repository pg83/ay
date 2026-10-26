package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	indexedMagic      = "AYIDX\x00\x00\x01"
	indexedHeaderSize = 64
	indexedEntrySize  = 32
	indexedKindDir    = 2
)

type indexedEntry struct {
	nameOff uint32
	nameLen uint32
	kind    uint32
	mode    uint32
	a       uint64
	b       uint64
}

type IndexedTree struct {
	entries []indexedEntry
	names   []byte
	blob    []byte
}

func isIndexedFile(path string) bool {
	f, err := os.Open(path)

	if err != nil {
		return false
	}

	defer f.Close()

	var magic [len(indexedMagic)]byte

	n, _ := f.ReadAt(magic[:], 0)

	return n == len(magic) && string(magic[:]) == indexedMagic
}

func openIndexed(path string) (*IndexedTree, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	st, err := f.Stat()

	if err != nil {
		return nil, err
	}

	if st.Size() < indexedHeaderSize {
		return nil, fmt.Errorf("%s: not an indexed archive", path)
	}

	raw, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)

	if err != nil {
		return nil, fmt.Errorf("mmap %s: %w", path, err)
	}

	if string(raw[:len(indexedMagic)]) != indexedMagic {
		return nil, fmt.Errorf("%s: not an indexed archive", path)
	}

	u64 := func(off int) uint64 {
		return binary.LittleEndian.Uint64(raw[off:])
	}

	count, namesOff, namesSize, dataOff, dataSize := u64(8), u64(16), u64(24), u64(32), u64(40)

	if count == 0 || indexedHeaderSize+count*indexedEntrySize > namesOff || namesOff+namesSize > dataOff || dataOff+dataSize > uint64(len(raw)) {
		return nil, fmt.Errorf("%s: corrupt indexed archive header", path)
	}

	return &IndexedTree{
		entries: unsafe.Slice((*indexedEntry)(unsafe.Pointer(&raw[indexedHeaderSize])), count),
		names:   raw[namesOff : namesOff+namesSize],
		blob:    raw[dataOff : dataOff+dataSize],
	}, nil
}

func (t *IndexedTree) name(i int) string {
	e := &t.entries[i]

	return unsafe.String(unsafe.SliceData(t.names[e.nameOff:]), e.nameLen)
}

func (t *IndexedTree) isDir(i int) bool {
	return t.entries[i].kind == indexedKindDir
}

func (t *IndexedTree) children(i int) (first, count int) {
	e := &t.entries[i]

	return int(e.a), int(e.b)
}

func (t *IndexedTree) data(i int) []byte {
	e := &t.entries[i]

	return t.blob[e.a : e.a+e.b : e.a+e.b]
}

func (t *IndexedTree) child(dir int, name string) int {
	first, count := t.children(dir)
	lo, hi := first, first+count

	for lo < hi {
		mid := int(uint(lo+hi) >> 1)

		if t.name(mid) < name {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	if lo < first+count && t.name(lo) == name {
		return lo
	}

	return -1
}

func (t *IndexedTree) lookup(rel string) int {
	i := 0

	for rel != "" {
		if !t.isDir(i) {
			return -1
		}

		name := rel

		if slash := strings.IndexByte(rel, '/'); slash >= 0 {
			name, rel = rel[:slash], rel[slash+1:]
		} else {
			rel = ""
		}

		if i = t.child(i, name); i < 0 {
			return -1
		}
	}

	return i
}
