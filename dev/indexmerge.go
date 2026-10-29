//go:build ignore

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
)

const usage = `usage: indexmerge --out <merged.indexed> <slice.indexed>...
  the union of .indexed archives (dev/index.go): a path present in several
  inputs must agree on kind, mode and size and is taken from the first one;
  an empty file is a slice placeholder and yields to a non-empty one
`

const (
	header      = 64
	entrySize   = 32
	kindFile    = 1
	kindDir     = 2
	modeSymlink = 1 << 16
	align       = 4096
)

var magic = []byte("AYIDX\x00\x00\x01")

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "indexmerge: "+format+"\n", args...)
	os.Exit(1)
}

func check(err error) {
	if err != nil {
		die("%v", err)
	}
}

type entry struct {
	name string
	kind uint32
	mode uint32
	a, b uint64
}

type input struct {
	path      string
	file      *os.File
	entries   []entry
	dataOff   int64
	canonical map[uint64]int
	merged    map[uint64]*dir
	err       error
}

func (in *input) load() {
	f, err := os.Open(in.path)

	if err != nil {
		in.err = err

		return
	}

	in.file = f

	head := make([]byte, header)

	if _, err := f.ReadAt(head, 0); err != nil {
		in.err = fmt.Errorf("%s: %w", in.path, err)

		return
	}

	if !bytes.Equal(head[:len(magic)], magic) {
		in.err = fmt.Errorf("%s: not an indexed archive", in.path)

		return
	}

	count := binary.LittleEndian.Uint64(head[8:])
	namesOff := int64(binary.LittleEndian.Uint64(head[16:]))
	namesSize := int64(binary.LittleEndian.Uint64(head[24:]))

	in.dataOff = int64(binary.LittleEndian.Uint64(head[32:]))

	raw := make([]byte, int64(count)*entrySize)
	names := make([]byte, namesSize)

	if _, err := f.ReadAt(raw, header); err != nil {
		in.err = fmt.Errorf("%s: %w", in.path, err)

		return
	}

	if _, err := f.ReadAt(names, namesOff); err != nil {
		in.err = fmt.Errorf("%s: %w", in.path, err)

		return
	}

	in.entries = make([]entry, count)
	in.canonical = map[uint64]int{}

	for i := range in.entries {
		e := raw[i*entrySize:]
		off, size := binary.LittleEndian.Uint32(e), binary.LittleEndian.Uint32(e[4:])

		in.entries[i] = entry{
			name: string(names[off : off+size]),
			kind: binary.LittleEndian.Uint32(e[8:]),
			mode: binary.LittleEndian.Uint32(e[12:]),
			a:    binary.LittleEndian.Uint64(e[16:]),
			b:    binary.LittleEndian.Uint64(e[24:]),
		}
	}

	for i, e := range in.entries {
		if e.kind != kindDir || e.b == 0 {
			continue
		}

		if prev, ok := in.canonical[e.a]; !ok || in.entries[prev].mode&modeSymlink != 0 && e.mode&modeSymlink == 0 {
			in.canonical[e.a] = i
		}
	}
}

type ref struct {
	in        int
	off, size uint64
	outOff    uint64
	placed    bool
}

type child struct {
	dir  *dir
	file *ref
	mode uint32
}

type dir struct {
	mode     uint32
	children map[string]child
	first    int
	count    int
}

func newDir(mode uint32) *dir {
	return &dir{mode: mode, children: map[string]child{}}
}

type alias struct {
	parent *dir
	name   string
	target uint64
	mode   uint32
}

type merger struct {
	inputs []*input
	refs   map[[3]uint64]*ref
}

func (m *merger) fileRef(in int, off, size uint64) *ref {
	key := [3]uint64{uint64(in), off, size}

	if r, ok := m.refs[key]; ok {
		return r
	}

	r := &ref{in: in, off: off, size: size}

	m.refs[key] = r

	return r
}

func (m *merger) walk(i int, in *input, index int, into *dir, where string, aliases *[]alias) {
	e := in.entries[index]

	if e.b > 0 {
		in.merged[e.a] = into
	}

	for k := int(e.a); k < int(e.a+e.b); k++ {
		c := in.entries[k]
		at := where + "/" + c.name

		if c.kind != kindDir {
			have, ok := into.children[c.name]

			switch {
			case !ok || have.file != nil && have.file.size == 0 && c.b > 0:
				into.children[c.name] = child{file: m.fileRef(i, c.a, c.b), mode: c.mode}
			case have.file == nil:
				die("%s: %s disagrees with an earlier input", in.path, at)
			case c.b == 0 || have.file.size == c.b && have.mode == c.mode:
			default:
				die("%s: %s disagrees with an earlier input", in.path, at)
			}

			continue
		}

		if c.b > 0 && in.canonical[c.a] != k {
			*aliases = append(*aliases, alias{parent: into, name: c.name, target: c.a, mode: c.mode})

			continue
		}

		have, ok := into.children[c.name]

		switch {
		case !ok:
			have = child{dir: newDir(c.mode), mode: c.mode}
			into.children[c.name] = have
		case have.dir == nil || have.mode != c.mode:
			die("%s: %s disagrees with an earlier input", in.path, at)
		}

		m.walk(i, in, k, have.dir, at, aliases)
	}
}

type slot struct {
	name string
	c    child
}

func layout(root *dir) []slot {
	order := []slot{{c: child{dir: root, mode: root.mode}}}
	queued := map[*dir]bool{root: true}
	queue := []*dir{root}

	for len(queue) > 0 {
		var following []*dir

		for _, d := range queue {
			names := make([]string, 0, len(d.children))

			for name := range d.children {
				names = append(names, name)
			}

			sort.Strings(names)

			d.first, d.count = len(order), len(names)

			for _, name := range names {
				c := d.children[name]

				order = append(order, slot{name: name, c: c})

				if c.dir != nil && !queued[c.dir] {
					queued[c.dir] = true
					following = append(following, c.dir)
				}
			}
		}

		queue = following
	}

	return order
}

type run struct {
	inOff, outOff, size uint64
}

func main() {
	out := ""

	var paths []string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--out":
			if i+1 >= len(os.Args) {
				die("missing value for --out")
			}

			i++
			out = os.Args[i]
		case "-h", "--help":
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		default:
			paths = append(paths, os.Args[i])
		}
	}

	if out == "" || len(paths) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	m := &merger{refs: map[[3]uint64]*ref{}}

	var wg sync.WaitGroup

	for _, path := range paths {
		in := &input{path: path, merged: map[uint64]*dir{}}

		m.inputs = append(m.inputs, in)
		wg.Add(1)

		go func() {
			defer wg.Done()

			in.load()
		}()
	}

	wg.Wait()

	for _, in := range m.inputs {
		check(in.err)
	}

	root := newDir(m.inputs[0].entries[0].mode)

	for i, in := range m.inputs {
		if in.entries[0].kind != kindDir {
			die("%s: root is not a directory", in.path)
		}

		var aliases []alias

		m.walk(i, in, 0, root, "", &aliases)

		for _, a := range aliases {
			target := in.merged[a.target]

			if target == nil {
				die("%s: %s links outside the archive", in.path, a.name)
			}

			have, ok := a.parent.children[a.name]

			if !ok {
				a.parent.children[a.name] = child{dir: target, mode: a.mode}

				continue
			}

			if have.dir != target || have.mode != a.mode {
				die("%s: link %s disagrees with an earlier input", in.path, a.name)
			}
		}
	}

	order := layout(root)

	perInput := make([][]*ref, len(m.inputs))

	for _, s := range order {
		if r := s.c.file; r != nil && !r.placed {
			r.placed = true
			perInput[r.in] = append(perInput[r.in], r)
		}
	}

	runs := make([][]run, len(m.inputs))

	var dataSize uint64

	for i, refs := range perInput {
		sort.Slice(refs, func(a, b int) bool {
			if refs[a].off != refs[b].off {
				return refs[a].off < refs[b].off
			}

			return refs[a].size < refs[b].size
		})

		for _, r := range refs {
			if n := len(runs[i]); n > 0 && r.off <= runs[i][n-1].inOff+runs[i][n-1].size {
				last := &runs[i][n-1]

				if end := r.off + r.size; end > last.inOff+last.size {
					grow := end - (last.inOff + last.size)

					last.size += grow
					dataSize += grow
				}

				r.outOff = last.outOff + (r.off - last.inOff)

				continue
			}

			runs[i] = append(runs[i], run{inOff: r.off, outOff: dataSize, size: r.size})
			r.outOff = dataSize
			dataSize += r.size
		}
	}

	var names []byte

	entries := make([]byte, 0, len(order)*entrySize)

	for _, s := range order {
		entries = binary.LittleEndian.AppendUint32(entries, uint32(len(names)))
		entries = binary.LittleEndian.AppendUint32(entries, uint32(len(s.name)))
		names = append(names, s.name...)

		if s.c.dir != nil {
			entries = binary.LittleEndian.AppendUint32(entries, kindDir)
			entries = binary.LittleEndian.AppendUint32(entries, s.c.mode)
			entries = binary.LittleEndian.AppendUint64(entries, uint64(s.c.dir.first))
			entries = binary.LittleEndian.AppendUint64(entries, uint64(s.c.dir.count))
		} else {
			entries = binary.LittleEndian.AppendUint32(entries, kindFile)
			entries = binary.LittleEndian.AppendUint32(entries, s.c.mode)
			entries = binary.LittleEndian.AppendUint64(entries, s.c.file.outOff)
			entries = binary.LittleEndian.AppendUint64(entries, s.c.file.size)
		}
	}

	namesOff := uint64(header + len(entries))
	dataOff := (namesOff + uint64(len(names)) + align - 1) / align * align

	head := make([]byte, header)

	copy(head, magic)
	binary.LittleEndian.PutUint64(head[8:], uint64(len(order)))
	binary.LittleEndian.PutUint64(head[16:], namesOff)
	binary.LittleEndian.PutUint64(head[24:], uint64(len(names)))
	binary.LittleEndian.PutUint64(head[32:], dataOff)
	binary.LittleEndian.PutUint64(head[40:], dataSize)

	f, err := os.Create(out)
	check(err)
	check(f.Truncate(int64(dataOff + dataSize)))

	for _, chunk := range []struct {
		off  uint64
		data []byte
	}{{0, head}, {header, entries}, {namesOff, names}} {
		_, err := f.WriteAt(chunk.data, int64(chunk.off))
		check(err)
	}

	errs := make([]error, len(m.inputs))

	for i, in := range m.inputs {
		wg.Add(1)

		go func() {
			defer wg.Done()

			buf := make([]byte, 1<<20)

			for _, r := range runs[i] {
				src := io.NewSectionReader(in.file, in.dataOff+int64(r.inOff), int64(r.size))
				dst := io.NewOffsetWriter(f, int64(dataOff+r.outOff))

				if n, err := io.CopyBuffer(dst, src, buf); err != nil || n != int64(r.size) {
					errs[i] = fmt.Errorf("%s: copied %d of %d bytes: %v", in.path, n, r.size, err)

					return
				}
			}
		}()
	}

	wg.Wait()

	for _, err := range errs {
		check(err)
	}

	check(f.Close())
}
