package main

import "github.com/zeebo/xxh3"

var parseJoinScratch []byte

type FS interface {
	root() string
	listdir(dir STR) DirView
	dirHas(v DirView, name string) (present bool, isDir bool)

	exists(prefix STR, suffix string) (present bool, isDir bool)
	existsID(prefix, rel STR) (present bool, isDir bool)
	isFile(prefix STR, suffix string) bool
	isFileClean(prefix STR, suffix string) bool
	isFileID(dir, name STR) bool
	isFilePathClean(rel string) bool
	isFileVFS(v VFS) bool
	noteFile(v VFS)
	isReadFile(rel STR) bool
	isDir(prefix STR, suffix string) bool
	sourcePathExists(rel string) bool

	resolveSourceUnder(prefix, target STR) STR
	resolveSourceUnderClean(prefix, target STR, targetClean bool) STR

	read(rel string) [][]byte
	readPath(rel STR) [][]byte

	resourceBundle(rel STR) (map[string]ResourceBundleEntry, bool)
	storeResourceBundle(rel STR, bundle map[string]ResourceBundleEntry)

	walk(rel string, visit func(rel string, isDir bool) bool)
	walkDirID(root STR, visit func(rel string, dir STR, isDir bool) bool)
	isFileVFSAny(v VFS) bool
	testPathReadError(rel string) error

	confRoot() string
}

func concatChunks(chunks [][]byte) []byte {
	if len(chunks) == 1 {
		return chunks[0]
	}

	n := 0

	for _, chunk := range chunks {
		n += len(chunk)
	}

	data := make([]byte, 0, n)

	for _, chunk := range chunks {
		data = append(data, chunk...)
	}

	return data
}

func joinChunksScratch(chunks [][]byte, sentinel bool) []byte {
	if len(chunks) == 1 && !sentinel {
		return chunks[0]
	}

	n := 0

	for _, chunk := range chunks {
		n += len(chunk)
	}

	if sentinel {
		n++
	}

	if cap(parseJoinScratch) < n {
		parseJoinScratch = make([]byte, 0, max(n, 2*cap(parseJoinScratch)))
	}

	data := parseJoinScratch[:0]

	for _, chunk := range chunks {
		data = append(data, chunk...)
	}

	if sentinel {
		data = append(data, 0)
	}

	return data
}

func contentHashChunks(chunks [][]byte) uint64 {
	var hash uint64

	for _, chunk := range chunks {
		hash ^= xxh3.Hash(chunk)
	}

	return hash
}

func contentHashBytes(data []byte) uint64 {
	if len(data) <= readChunkSize {
		return xxh3.Hash(data)
	}

	return xxh3.Hash(data[:readChunkSize]) ^ xxh3.Hash(data[readChunkSize:])
}

func dirKey(dir string) STR {
	return internStr(cleanRel(dir))
}

type DirView struct {
	dir   STR
	node  int32
	names []uint32
}

func (v DirView) listable() bool {
	return v.names != nil
}
