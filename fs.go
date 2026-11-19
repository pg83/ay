package main

import (
	"syscall"

	"github.com/zeebo/xxh3"
)

type Backend interface {
	readFile(rel string) ([][]byte, syscall.Errno)
	readDir(rel string, sink DirSink) syscall.Errno
	exists(rel string) bool
	readError(rel string) error
	localRoot() string
}

type DirSink interface {
	dirBegin(limit int)
	dirEntry(name []byte, isDir bool)
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
