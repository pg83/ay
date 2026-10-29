//go:build ignore

package main

import (
	"archive/tar"
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

const usage = `usage: index [--root-marker <file>] < archive.tar > slice.indexed
  the uncompressed resource tarball on stdin as the .indexed archive ay reads
  (fs_indexed.go): a header, the entry table and the names, then the file
  contents laid out flat; --root-marker re-roots at the shallowest directory
  holding that file
`

const (
	header       = 64
	entrySize    = 32
	kindFileOut  = 1
	kindDirOut   = 2
	modeSymlink  = 1 << 16
	align        = 4096
	maxLinkDepth = 40
)

var magic = []byte("AYIDX\x00\x00\x01")

type kind int

const (
	kindFile kind = iota
	kindDir
	kindSymlink
	kindHardlink
)

type node struct {
	kind     kind
	mode     uint32
	names    []string
	children map[string]*node
	link     string
	size     int64
	spool    int64
	offset   int64
	alias    *node
	first    int
	count    int
}

func newDir(mode uint32) *node {
	return &node{kind: kindDir, mode: mode, children: map[string]*node{}}
}

func (n *node) get(name string) *node {
	return n.children[name]
}

func (n *node) set(name string, child *node) {
	if _, ok := n.children[name]; !ok {
		n.names = append(n.names, name)
	}

	n.children[name] = child
}

func (n *node) remove(name string) {
	delete(n.children, name)

	for i, have := range n.names {
		if have == name {
			n.names = append(n.names[:i], n.names[i+1:]...)

			break
		}
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "index: "+format+"\n", args...)
	os.Exit(1)
}

func check(err error) {
	if err != nil {
		die("%v", err)
	}
}

func clean(name string) string {
	return strings.TrimLeft(path.Clean("/"+name), "/")
}

func join(a, b string) string {
	if a == "" {
		return b
	}

	if b == "" {
		return a + "/"
	}

	return a + "/" + b
}

type tree struct {
	root *node
}

func (t *tree) parent(dir string) *node {
	n := t.root

	if dir == "" {
		return n
	}

	for _, part := range strings.Split(dir, "/") {
		child := n.get(part)

		if child == nil || child.kind != kindDir {
			child = newDir(0o755)
			n.set(part, child)
		}

		n = child
	}

	return n
}

func (t *tree) add(name string, n *node) {
	if name == "" {
		return
	}

	dir, base := path.Split(name)
	parent := t.parent(strings.TrimSuffix(dir, "/"))

	if previous := parent.get(base); previous != nil && previous.kind == kindDir && n.kind == kindDir {
		previous.mode = n.mode

		return
	}

	parent.set(base, n)
}

func (t *tree) lookup(p string, depth int) *node {
	if depth > maxLinkDepth {
		return nil
	}

	n := t.root
	done := ""

	var parts []string

	if p != "" {
		parts = strings.Split(p, "/")
	}

	for i, part := range parts {
		if n.kind != kindDir {
			return nil
		}

		child := n.get(part)

		if child == nil {
			return nil
		}

		here := join(done, part)

		switch child.kind {
		case kindSymlink:
			target := child.link

			if strings.HasPrefix(target, "/") {
				return nil
			}

			joined := join(done, target)

			if strings.HasPrefix(path.Clean(joined), "..") {
				return nil
			}

			resolved := clean(joined)
			rest := strings.Join(parts[i+1:], "/")

			if rest != "" {
				return t.lookup(join(resolved, rest), depth+1)
			}

			return t.lookup(resolved, depth+1)
		case kindHardlink:
			return t.lookup(child.link, depth+1)
		}

		n, done = child, here
	}

	return n
}

func (t *tree) findRoot(marker string) string {
	type item struct {
		path string
		node *node
	}

	queue := []item{{"", t.root}}

	for len(queue) > 0 {
		var following []item

		for _, it := range queue {
			if child := it.node.get(marker); child != nil && child.kind == kindFile {
				return it.path
			}

			names := append([]string(nil), it.node.names...)

			sort.Strings(names)

			for _, name := range names {
				if sub := it.node.children[name]; sub.kind == kindDir {
					following = append(following, item{join(it.path, name), sub})
				}
			}
		}

		queue = following
	}

	die("no %s in the archive", marker)

	return ""
}

func (t *tree) resolveLinks(n *node, p string) {
	for _, name := range append([]string(nil), n.names...) {
		child := n.children[name]
		here := join(p, name)

		switch child.kind {
		case kindSymlink, kindHardlink:
			target := t.lookup(here, 0)

			if target == nil || target.kind != kindFile && target.kind != kindDir {
				n.remove(name)

				continue
			}

			child.alias = target
		case kindDir:
			t.resolveLinks(child, here)
		}
	}
}

func final(n *node) *node {
	if n.alias != nil {
		return n.alias
	}

	return n
}

type slot struct {
	node *node
	name string
	link bool
}

func layout(top *node) []slot {
	order := []slot{{node: top}}
	queued := map[*node]bool{top: true}
	queue := []*node{top}

	for len(queue) > 0 {
		var following []*node

		for _, dir := range queue {
			children := make([]slot, 0, len(dir.names))

			for _, name := range dir.names {
				child := dir.children[name]
				children = append(children, slot{node: final(child), name: name, link: child.kind == kindSymlink})
			}

			sort.SliceStable(children, func(i, j int) bool { return children[i].name < children[j].name })

			dir.first, dir.count = len(order), len(children)

			for _, child := range children {
				order = append(order, child)

				if child.node.kind == kindDir && !queued[child.node] {
					queued[child.node] = true
					following = append(following, child.node)
				}
			}
		}

		queue = following
	}

	return order
}

func main() {
	marker := ""

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--root-marker":
			if i+1 >= len(os.Args) {
				die("missing value for --root-marker")
			}

			i++
			marker = os.Args[i]
		default:
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
	}

	spool, err := os.CreateTemp("", "ay-index-")
	check(err)
	check(os.Remove(spool.Name()))

	spooled := bufio.NewWriterSize(spool, 1<<20)

	var files []*node
	var spoolSize int64

	t := &tree{root: newDir(0o755)}
	r := tar.NewReader(bufio.NewReaderSize(os.Stdin, 1<<20))

	for {
		h, err := r.Next()

		if err == io.EOF {
			break
		}

		check(err)

		name := clean(h.Name)
		mode := uint32(h.Mode) & 0o7777

		var n *node

		switch h.Typeflag {
		case tar.TypeDir:
			n = newDir(0o755)
		case tar.TypeSymlink:
			n = &node{kind: kindSymlink, mode: mode, link: h.Linkname}
		case tar.TypeLink:
			n = &node{kind: kindHardlink, mode: mode, link: clean(h.Linkname)}
		case tar.TypeReg, tar.TypeCont, tar.TypeGNUSparse:
			n = &node{kind: kindFile, mode: mode, size: h.Size, spool: spoolSize}

			written, err := io.Copy(spooled, r)
			check(err)

			if written != h.Size {
				die("%s: archive ended early", h.Name)
			}

			spoolSize += written
		default:
			continue
		}

		if name == "" {
			continue
		}

		if n.kind == kindFile {
			files = append(files, n)
		}

		t.add(name, n)
	}

	check(spooled.Flush())

	root := ""

	if marker != "" {
		root = t.findRoot(marker)
	}

	t.resolveLinks(t.root, "")

	top := t.root

	if root != "" {
		top = t.lookup(root, 0)
	}

	order := layout(top)
	reachable := map[*node]bool{}

	for _, s := range order {
		if s.node.kind == kindFile {
			reachable[s.node] = true
		}
	}

	var dataSize int64
	var written []*node

	for _, n := range files {
		if reachable[n] {
			n.offset = dataSize
			dataSize += n.size
			written = append(written, n)
		}
	}

	var names []byte

	entries := make([]byte, 0, len(order)*entrySize)

	for _, s := range order {
		offset := len(names)

		names = append(names, s.name...)

		mode := s.node.mode

		if s.link {
			mode |= modeSymlink
		}

		entries = binary.LittleEndian.AppendUint32(entries, uint32(offset))
		entries = binary.LittleEndian.AppendUint32(entries, uint32(len(s.name)))

		if s.node.kind == kindDir {
			entries = binary.LittleEndian.AppendUint32(entries, kindDirOut)
			entries = binary.LittleEndian.AppendUint32(entries, mode)
			entries = binary.LittleEndian.AppendUint64(entries, uint64(s.node.first))
			entries = binary.LittleEndian.AppendUint64(entries, uint64(s.node.count))
		} else {
			entries = binary.LittleEndian.AppendUint32(entries, kindFileOut)
			entries = binary.LittleEndian.AppendUint32(entries, mode)
			entries = binary.LittleEndian.AppendUint64(entries, uint64(s.node.offset))
			entries = binary.LittleEndian.AppendUint64(entries, uint64(s.node.size))
		}
	}

	namesOffset := int64(header + len(entries))
	dataOffset := (namesOffset + int64(len(names)) + align - 1) / align * align

	head := make([]byte, header)

	copy(head, magic)
	binary.LittleEndian.PutUint64(head[8:], uint64(len(order)))
	binary.LittleEndian.PutUint64(head[16:], uint64(namesOffset))
	binary.LittleEndian.PutUint64(head[24:], uint64(len(names)))
	binary.LittleEndian.PutUint64(head[32:], uint64(dataOffset))
	binary.LittleEndian.PutUint64(head[40:], uint64(dataSize))

	out := bufio.NewWriterSize(os.Stdout, 1<<20)

	for _, chunk := range [][]byte{head, entries, names, make([]byte, dataOffset-namesOffset-int64(len(names)))} {
		_, err := out.Write(chunk)
		check(err)
	}

	check(out.Flush())

	for i := 0; i < len(written); {
		start, size := written[i].spool, written[i].size

		for i++; i < len(written) && written[i].spool == start+size; i++ {
			size += written[i].size
		}

		_, err := spool.Seek(start, io.SeekStart)
		check(err)

		copied, err := os.Stdout.ReadFrom(io.LimitReader(spool, size))
		check(err)

		if copied != size {
			die("spool ended early")
		}
	}
}
