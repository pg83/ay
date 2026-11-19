package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"slices"
	"syscall"
	"unsafe"

	"github.com/zeebo/xxh3"
)

const (
	treeImageMagic      = "AYTREE\x00\x01"
	treeImageVersion    = 1
	treeImageHeaderSize = 1 << 16
	treeImageAlign      = 1 << 16
	treeImageMaxSection = 64
)

const (
	treeSectionStrText TreeSection = iota + 1
	treeSectionStrPos
	treeSectionStrLo
	treeSectionStrIndexKeys
	treeSectionStrIndexValues
	treeSectionStrOverflow
	treeSectionFileNo
	treeSectionFileHash
	treeSectionFileParsed
	treeSectionDirectives
	treeSectionParsedExtras
	treeSectionTokenSpans
	treeSectionTokenBytes
)

type TreeSession struct {
	image *TreeImage
	out   string
	tree  Tree
	live  *LiveTree
}

func openTreeSession(image, out string) *TreeSession {
	s := &TreeSession{out: out}

	if image != "" {
		s.image = openTreeImage(image)
		s.image.attachStrings()
	}

	return s
}

func (s *TreeSession) finish(code int, verbose bool) {
	if s.out != "" && code == 0 {
		writeTreeImage(s.out, s.tree)
	}

	if !verbose || s.out == "" && s.image == nil {
		return
	}

	t := &internTable

	fmt.Fprintf(os.Stderr, "tree image: %d strings from the image, %d new, %d bytes of text\n", t.imageCount, t.count-max(t.imageCount, t.constCount), t.bytes.used)

	if s.live != nil {
		fmt.Fprintf(os.Stderr, "tree image: %d files read, %d of them parsed for includes, %d directories listed\n", s.live.reads, s.live.parses, s.live.listings)
	}
}

func binaryHash() uint64 {
	return xxh3.Hash(throw2(os.ReadFile(throw2(os.Executable()))))
}

func mmapedSlice[T any](data []byte) []T {
	var zero T

	return unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(data))), len(data)/int(unsafe.Sizeof(zero)))
}

func (img *TreeImage) files() *MmapedFiles {
	return &MmapedFiles{
		fileNo:     mmapedSlice[uint32](img.mapped(treeSectionFileNo)),
		hashes:     mmapedSlice[uint64](img.mapped(treeSectionFileHash)),
		parsed:     mmapedSlice[MmapedParsed](img.mapped(treeSectionFileParsed)),
		directives: mmapedSlice[IncludeDirective](img.mapped(treeSectionDirectives)),
		extras:     mmapedSlice[MmapedExtra](img.mapped(treeSectionParsedExtras)),
		tokenSpans: mmapedSlice[MmapedSpan](img.mapped(treeSectionTokenSpans)),
		tokenBytes: img.mapped(treeSectionTokenBytes),
	}
}

type TreeSection uint32

type TreeSectionEntry struct {
	kind TreeSection
	off  uint64
	size uint64
}

type TreeImage struct {
	file     *os.File
	sections [treeImageMaxSection]TreeSectionEntry
	strCount uint32
}

func (img *TreeImage) section(kind TreeSection) TreeSectionEntry {
	entry := img.sections[kind]

	if entry.kind != kind {
		throwFmt("tree image %s has no section %d", img.file.Name(), kind)
	}

	return entry
}

func (img *TreeImage) mapped(kind TreeSection) []byte {
	entry := img.section(kind)

	if entry.size == 0 {
		return nil
	}

	return throw2(syscall.Mmap(int(img.file.Fd()), int64(entry.off), int(entry.size), syscall.PROT_READ, syscall.MAP_SHARED))
}

func internConstHash() uint64 {
	return xxh3.Hash(unsafe.Slice((*byte)(internTable.lo.base), int(internTable.constCount)*8))
}

func openTreeImage(path string) *TreeImage {
	img := &TreeImage{file: throw2(os.Open(path))}
	header := make([]byte, treeImageHeaderSize)

	throw2(img.file.ReadAt(header, 0))

	if string(header[:len(treeImageMagic)]) != treeImageMagic || binary.LittleEndian.Uint32(header[8:]) != treeImageVersion {
		throwFmt("%s is not a tree image of version %d", path, treeImageVersion)
	}

	count := int(binary.LittleEndian.Uint32(header[12:]))

	img.strCount = binary.LittleEndian.Uint32(header[16:])

	if constCount, constBytes, constHash := binary.LittleEndian.Uint32(header[20:]), binary.LittleEndian.Uint64(header[24:]), binary.LittleEndian.Uint64(header[32:]); constCount != internTable.constCount || constBytes != uint64(internTable.constBytes) || constHash != internConstHash() || binary.LittleEndian.Uint64(header[40:]) != binaryHash() {
		throwFmt("tree image %s was written by another build of ay", path)
	}

	for i := range count {
		at := header[64+i*24:]
		entry := TreeSectionEntry{kind: TreeSection(binary.LittleEndian.Uint32(at)), off: binary.LittleEndian.Uint64(at[8:]), size: binary.LittleEndian.Uint64(at[16:])}

		img.sections[entry.kind] = entry
	}

	return img
}

func (img *TreeImage) attachStrings() {
	t := &internTable

	if t.count != t.constCount || t.bytes.used != t.constBytes {
		throwFmt("tree image must be attached before any string is interned")
	}

	text, pos, lo := img.section(treeSectionStrText), img.section(treeSectionStrPos), img.section(treeSectionStrLo)

	t.bytes.mapFile(img.file, text.off, int(text.size))
	t.pos.mapFile(img.file, pos.off, int(pos.size))
	t.lo.mapFile(img.file, lo.off, int(lo.size))
	t.count = img.strCount
	t.imageCount = img.strCount
	t.mapped = uint32(min(t.pos.mapped, t.lo.mapped) / 8)

	keys := mmapedSlice[uint64](img.mapped(treeSectionStrIndexKeys))

	t.frozen = &MmapedIntMap[STR]{keys: keys, values: mmapedSlice[STR](img.mapped(treeSectionStrIndexValues)), mask: uint64(len(keys) - 1)}

	overflow := img.mapped(treeSectionStrOverflow)

	for i := 0; i < len(overflow); i += 4 {
		id := STR(binary.LittleEndian.Uint32(overflow[i:]))

		t.overflow[id.string()] = id
	}
}

type TreeImageWriter struct {
	file    *os.File
	off     uint64
	entries []TreeSectionEntry
}

func (w *TreeImageWriter) section(kind TreeSection, data []byte) {
	w.entries = append(w.entries, TreeSectionEntry{kind: kind, off: w.off, size: uint64(len(data))})

	throw2(w.file.WriteAt(data, int64(w.off)))

	w.off = (w.off + uint64(len(data)) + treeImageAlign - 1) &^ (treeImageAlign - 1)
}

func sliceBytesOf[T any](s []T) []byte {
	var zero T

	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(s))), len(s)*int(unsafe.Sizeof(zero)))
}

func internIndexForImage() (keys []uint64, values []STR) {
	t := &internTable

	if t.frozen == nil {
		return t.ids.keys, t.ids.values
	}

	frozen := 0

	for _, k := range t.frozen.keys {
		if k != 0 {
			frozen++
		}
	}

	merged := newIntMap[STR](frozen + t.ids.count)

	for i, k := range t.frozen.keys {
		if k != 0 {
			merged.put(k, t.frozen.values[i])
		}
	}

	for i, k := range t.ids.keys {
		if k != 0 && t.frozen.get(k) == nil {
			merged.put(k, t.ids.values[i])
		}
	}

	return merged.keys, merged.values
}

func writeTreeImage(path string, tree Tree) {
	t := &internTable
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	w := &TreeImageWriter{file: throw2(os.Create(tmp)), off: treeImageHeaderSize}

	w.section(treeSectionStrText, unsafe.Slice((*byte)(t.bytes.base), t.bytes.used))
	w.section(treeSectionStrPos, unsafe.Slice((*byte)(t.pos.base), int(t.count)*8))
	w.section(treeSectionStrLo, unsafe.Slice((*byte)(t.lo.base), int(t.count)*8))

	keys, values := internIndexForImage()

	w.section(treeSectionStrIndexKeys, sliceBytesOf(keys))
	w.section(treeSectionStrIndexValues, sliceBytesOf(values))

	overflow := make([]STR, 0, len(t.overflow))

	for _, id := range t.overflow {
		overflow = append(overflow, id)
	}

	slices.Sort(overflow)
	w.section(treeSectionStrOverflow, sliceBytesOf(overflow))

	if tree != nil {
		writeTreeFiles(w, tree)
	}

	header := make([]byte, treeImageHeaderSize)

	copy(header, treeImageMagic)
	binary.LittleEndian.PutUint32(header[8:], treeImageVersion)
	binary.LittleEndian.PutUint32(header[12:], uint32(len(w.entries)))
	binary.LittleEndian.PutUint32(header[16:], t.count)
	binary.LittleEndian.PutUint32(header[20:], t.constCount)
	binary.LittleEndian.PutUint64(header[24:], uint64(t.constBytes))
	binary.LittleEndian.PutUint64(header[32:], internConstHash())
	binary.LittleEndian.PutUint64(header[40:], binaryHash())

	for i, entry := range w.entries {
		at := header[64+i*24:]

		binary.LittleEndian.PutUint32(at, uint32(entry.kind))
		binary.LittleEndian.PutUint64(at[8:], entry.off)
		binary.LittleEndian.PutUint64(at[16:], entry.size)
	}

	throw2(w.file.WriteAt(header, 0))
	throw(w.file.Truncate(int64(w.off)))
	throw(w.file.Close())
	throw(os.Rename(tmp, path))
}
