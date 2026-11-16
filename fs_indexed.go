//go:build aygate

package main

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
)

const (
	ownerRead = 0o400
	ownerExec = 0o100
)

type fsBackend struct {
	tree           *IndexedTree
	testReadErrors map[string]error
	testWalk       []testWalkItem
	confRootOnce   sync.Once
	confRootDir    string
}

func (fs *OsFS) initBackend() {
	fs.tree = throw2(openIndexed(fs.srcRoot))
	fs.testReadErrors = map[string]error{}
}

func (fs *OsFS) lookupRel(rel string) int {
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
		if !fs.tree.isDir(i) || fs.tree.mode(i)&ownerExec == 0 {
			return -1
		}

		name, rest, more := strings.Cut(rel, "/")

		if i = fs.tree.child(i, name); i < 0 || !more {
			return i
		}

		rel = rest
	}
}

func (fs *OsFS) nodeOf(rel string) int {
	if rel == "" || !pathIsClean(rel) {
		return fs.lookupRel(rel)
	}

	parent, name := splitDirName(rel)
	p := 0

	if parent != "" {
		row := dirRows.rowOf(interned(parent))

		if int(row) >= len(fs.dirs.s) || fs.dirs.s[row].dir == 0 {
			return fs.lookupRel(rel)
		}

		p = int(fs.dirs.s[row].node) - 1
	}

	if p < 0 || !fs.tree.isDir(p) || fs.tree.mode(p)&ownerExec == 0 {
		return -1
	}

	return fs.tree.child(p, name)
}

func (fs *OsFS) dirHas(v DirView, name string) (present bool, isDir bool) {
	if v.names == nil {
		return false, false
	}

	i := fs.tree.child(int(v.node)-1, name)

	if i < 0 {
		return false, false
	}

	return true, fs.tree.isDir(i)
}

func (fs *OsFS) readFileRel(rel string) [][]byte {
	i := fs.nodeOf(rel)

	if i < 0 {
		throwFmt("open %s: %v", rel, syscall.ENOENT)
	}

	if fs.tree.mode(i)&ownerRead == 0 {
		throwFmt("open %s: %v", rel, syscall.EACCES)
	}

	if fs.tree.isDir(i) {
		throwFmt("read %s: %v", rel, syscall.EISDIR)
	}

	data := fs.tree.data(i)
	n := min(len(data), readChunkSize)

	fs.readResult[0] = data[:n:n]

	if n == len(data) {
		return fs.readResult[:1]
	}

	fs.readResult[1] = data[n:]

	return fs.readResult[:2]
}

func (fs *OsFS) readDirViewRel(dir STR, rel string) DirView {
	i := fs.nodeOf(rel)

	if i < 0 || !fs.tree.isDir(i) || fs.tree.mode(i)&ownerRead == 0 {
		return DirView{node: int32(i + 1)}
	}

	first, count := fs.tree.children(i)

	if count == 0 {
		return DirView{dir: dir, node: int32(i + 1), names: emptyDirNames}
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

	return DirView{dir: dir, node: int32(i + 1), names: block[:count]}
}

func (fs *OsFS) sourcePathExists(rel string) bool {
	return fs.lookupRel(rel) >= 0
}

func (fs *OsFS) testPathReadError(rel string) error {
	if err, cached := fs.testReadErrors[rel]; cached {
		return err
	}

	err := fs.testPathReadErrorUncached(rel)

	fs.testReadErrors[rel] = err

	return err
}

type testWalkItem struct {
	node   int32
	parent int32
	exec   bool
}

func (fs *OsFS) testPathReadErrorUncached(rel string) error {
	t := fs.tree

	if i := fs.lookupRel(rel); i >= 0 && t.isDir(i) {
		if t.mode(i)&ownerRead == 0 {
			return nil
		}

		first, count := t.children(i)

		for c := first; c < first+count; c++ {
			if err := fs.testTreeReadError(rel, c, t.mode(i)&ownerExec != 0); err != nil {
				return err
			}
		}

		return nil
	}

	name := cleanRel(rel)

	if name == "" {
		return fs.testTreeReadError("", 0, true)
	}

	dir, base := splitDirName(name)
	p := fs.lookupRel(dir)

	if p < 0 || !t.isDir(p) || t.mode(p)&ownerRead == 0 {
		return nil
	}

	if c := t.child(p, base); c >= 0 {
		return fs.testTreeReadError(dir, c, t.mode(p)&ownerExec != 0)
	}

	return nil
}

func (fs *OsFS) testTreeReadError(prefix string, root int, exec bool) error {
	t := fs.tree

	if !t.isDir(root) {
		if exec && !strings.HasSuffix(t.name(root), ".pyc") && t.unreadable(root) {
			return fs.testTreeError(prefix, nil, root, -1)
		}

		return nil
	}

	if testWalkSkipsDir(t.name(root)) {
		return nil
	}

	queue := append(fs.testWalk[:0], testWalkItem{node: int32(root), parent: -1, exec: exec})

	for head := 0; head < len(queue); head++ {
		d := queue[head]

		if !d.exec || t.mode(int(d.node))&ownerRead == 0 {
			continue
		}

		childExec := t.mode(int(d.node))&ownerExec != 0
		first, count := t.children(int(d.node))

		for x := first; x < first+count; x++ {
			if t.isDir(x) {
				if !testWalkSkipsDir(t.name(x)) {
					queue = append(queue, testWalkItem{node: int32(x), parent: int32(head), exec: childExec})
				}

				continue
			}

			if childExec && !strings.HasSuffix(t.name(x), ".pyc") && t.unreadable(x) {
				fs.testWalk = queue[:0]

				return fs.testTreeError(prefix, queue, x, head)
			}
		}
	}

	fs.testWalk = queue[:0]

	return nil
}

func (fs *OsFS) testTreeError(prefix string, queue []testWalkItem, file, dir int) error {
	names := []string{fs.tree.name(file)}

	for at := dir; at >= 0; at = int(queue[at].parent) {
		names = append(names, fs.tree.name(int(queue[at].node)))
	}

	slices.Reverse(names)

	name := joinRel(cleanRel(joinRel(prefix, names[0])), strings.Join(names[1:], "/"))

	return &os.PathError{Op: "open", Path: fs.rootSlash + name, Err: syscall.EACCES}
}

func (t *IndexedTree) unreadable(i int) bool {
	return t.entries[i].mode&(indexedModeLink|ownerRead) == 0
}

func testWalkSkipsDir(name string) bool {
	return name == ".svn" || name == ".cache" || name == ".idea"
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
