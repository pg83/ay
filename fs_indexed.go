//go:build aygate

package main

import (
	"os"
	"path"
	"path/filepath"
	"sync"
	"syscall"
)

type fsBackend struct {
	tree         *IndexedTree
	readBuf      []byte
	confRootOnce sync.Once
	confRootDir  string
}

func (fs *OsFS) initBackend() {
	fs.tree = throw2(openIndexed(fs.srcRoot))
	fs.readBuf = make([]byte, readChunkSize)
}

func (fs *OsFS) lookupRel(rel string) int {
	if rel == "" || rel == "." {
		return 0
	}

	rel = path.Clean(rel)

	if rel == ".." || len(rel) > 2 && rel[:3] == "../" || rel[0] == '/' {
		return -1
	}

	return fs.tree.lookup(rel)
}

func (fs *OsFS) readFileRel(rel string) [][]byte {
	i := fs.lookupRel(rel)

	if i < 0 {
		throwFmt("open %s: %v", rel, syscall.ENOENT)
	}

	if fs.tree.isDir(i) {
		throwFmt("read %s: %v", rel, syscall.EISDIR)
	}

	data := fs.tree.data(i)
	n := copy(fs.readBuf, data)

	fs.readResult[0] = fs.readBuf[:n]

	if n == len(data) {
		return fs.readResult[:1]
	}

	fs.readResult[1] = data[n:]

	return fs.readResult[:2]
}

func (fs *OsFS) readDirViewRel(dir STR, rel string) DirView {
	i := fs.lookupRel(rel)

	if i < 0 || !fs.tree.isDir(i) {
		return DirView{}
	}

	first, count := fs.tree.children(i)

	if count == 0 {
		return DirView{dir: dir, names: emptyDirNames}
	}

	block := fs.dirNames.alloc(count)

	for k := range count {
		child := first + k
		id := internStr(fs.tree.name(child))
		isDir := fs.tree.isDir(child)
		packed := uint32(id) << 1

		if isDir {
			packed |= 1
		}

		block[k] = packed
		fs.dirEntries.put(splitMix64(uint32(dir), uint32(id)), isDir)
	}

	fs.dirNames.commit(count)

	return DirView{dir: dir, names: block[:count]}
}

func (fs *OsFS) sourcePathExists(rel string) bool {
	return fs.lookupRel(rel) >= 0
}

func (fs *OsFS) testPathReadError(string) error {
	return nil
}

func (fs *OsFS) confRoot() string {
	fs.confRootOnce.Do(func() {
		root := throw2(os.MkdirTemp("", "ay-gate-conf-"))

		atExit(func() { os.RemoveAll(root) })

		for _, rel := range []string{ymakeConfScript, "build/internal"} {
			fs.walk(rel, func(name string, isDir bool) bool {
				out := filepath.Join(root, name)

				if isDir {
					throw(os.MkdirAll(out, 0o755))

					return true
				}

				throw(os.MkdirAll(filepath.Dir(out), 0o755))
				throw(os.WriteFile(out, concatChunks(fs.read(name)), 0o644))

				return false
			})
		}

		fs.confRootDir = root
	})

	return fs.confRootDir
}
