package main

import (
	"os"
	"path"
	"strings"
	"syscall"
	"unsafe"
)

const (
	ownerRead = 0o400
	ownerExec = 0o100
)

type IndexedBackend struct {
	root       string
	tree       *IndexedTree
	readResult [2][]byte
}

func newIndexedBackend(root string) *IndexedBackend {
	return &IndexedBackend{root: root, tree: throw2(openIndexed(root))}
}

func (b *IndexedBackend) lookup(rel string) int {
	if rel == "" || rel == "." {
		return 0
	}

	if !pathIsClean(rel) {
		rel = path.Clean(rel)
	}

	if rel == ".." || len(rel) > 2 && rel[:3] == "../" || rel[0] == '/' {
		return -1
	}

	i := 0

	for {
		if !b.tree.isDir(i) || b.tree.mode(i)&ownerExec == 0 {
			return -1
		}

		name, rest, more := strings.Cut(rel, "/")

		if i = b.tree.child(i, name); i < 0 || !more {
			return i
		}

		rel = rest
	}
}

func (b *IndexedBackend) readFile(rel string) ([][]byte, syscall.Errno) {
	i := b.lookup(rel)

	switch {
	case i < 0:
		return nil, syscall.ENOENT
	case b.tree.mode(i)&ownerRead == 0:
		return nil, syscall.EACCES
	case b.tree.isDir(i):
		return nil, syscall.EISDIR
	}

	data := b.tree.data(i)
	n := min(len(data), readChunkSize)

	b.readResult[0] = data[:n:n]

	if n == len(data) {
		return b.readResult[:1], 0
	}

	b.readResult[1] = data[n:]

	return b.readResult[:2], 0
}

func (b *IndexedBackend) readDir(rel string, sink DirSink) syscall.Errno {
	i := b.lookup(rel)

	switch {
	case i < 0:
		return syscall.ENOENT
	case !b.tree.isDir(i):
		return syscall.ENOTDIR
	case b.tree.mode(i)&ownerRead == 0:
		return syscall.EACCES
	}

	first, count := b.tree.children(i)

	sink.dirBegin(count)

	for child := first; child < first+count; child++ {
		name := b.tree.name(child)

		sink.dirEntry(unsafe.Slice(unsafe.StringData(name), len(name)), b.tree.isDir(child))
	}

	return 0
}

func (b *IndexedBackend) exists(rel string) bool {
	return b.lookup(rel) >= 0
}

func (b *IndexedBackend) readError(rel string) error {
	if i := b.lookup(rel); i >= 0 && !b.tree.isDir(i) && b.tree.unreadable(i) {
		return &os.PathError{Op: "open", Path: b.root + "/" + rel, Err: syscall.EACCES}
	}

	return nil
}

func (b *IndexedBackend) localRoot() string {
	return ""
}

func (t *IndexedTree) unreadable(i int) bool {
	return t.entries[i].mode&(indexedModeLink|ownerRead) == 0
}
