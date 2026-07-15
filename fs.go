package main

import "github.com/zeebo/xxh3"

type FS interface {
	listdir(dir STR) DirView
	dirHas(v DirView, name string) (present bool, isDir bool)

	exists(prefix STR, suffix string) (present bool, isDir bool)
	isFile(prefix STR, suffix string) bool
	isFileClean(prefix STR, suffix string) bool
	isFilePathClean(rel string) bool
	isDir(prefix STR, suffix string) bool

	resolveSourceUnder(prefix, target STR) STR
	resolveSourceUnderClean(prefix, target STR, targetClean bool) STR

	// read accepts a clean source-relative path and returns chunks that remain
	// valid until the next read call.
	read(rel string) [][]byte
	readPath(rel STR) [][]byte

	walk(rel string, visit func(rel string, isDir bool) bool)

	contentHash(rel STR) uint64
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
	names []uint32
}

func (v DirView) listable() bool {
	return v.names != nil
}
