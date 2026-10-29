//go:build ignore

package main

import (
	"bufio"
	"container/heap"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const usage = `usage: sort [--in <file>] [--out <file>] [--chunk-bytes N]
  external sort of lines, so two normalized graphs merge-compare
`

const defaultChunkBytes = 256 << 20

type failure struct {
	err error
}

func throw(err error) {
	if err != nil {
		panic(failure{err})
	}
}

func throw2[T any](val T, err error) T {
	throw(err)

	return val
}

func throwFmt(format string, args ...any) {
	panic(failure{fmt.Errorf(format, args...)})
}

func arg(args []string, i int) string {
	if i >= len(args) {
		throwFmt("missing value for flag %q", args[i-1])
	}

	return args[i]
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) (code int) {
	defer func() {
		if rec := recover(); rec != nil {
			f, ok := rec.(failure)

			if !ok {
				panic(rec)
			}

			fmt.Fprintf(os.Stderr, "sort: %v\n", f.err)

			code = 1
		}
	}()

	var inPath, outPath string

	chunkBytes := defaultChunkBytes

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--in":
			i++
			inPath = arg(args, i)
		case "--out":
			i++
			outPath = arg(args, i)
		case "--chunk-bytes":
			i++
			chunkBytes = int(throw2(strconv.Atoi(arg(args, i))))
		case "-h", "--help":
			fmt.Fprint(os.Stderr, usage)

			return 2
		default:
			throwFmt("unknown argument %q", args[i])
		}
	}

	var in io.Reader

	if inPath == "" || inPath == "-" {
		in = os.Stdin
	} else {
		f := throw2(os.Open(inPath))

		defer func() { throw(f.Close()) }()

		in = f
	}

	var out io.Writer

	if outPath == "" || outPath == "-" {
		out = os.Stdout
	} else {
		f := throw2(os.Create(outPath))

		defer func() { throw(f.Close()) }()

		out = f
	}

	tmpBase := "."

	if outPath != "" && outPath != "-" {
		tmpBase = filepath.Dir(outPath)
	}

	tmpDir, err := os.MkdirTemp(tmpBase, "aysort-")

	if err != nil {
		tmpDir = throw2(os.MkdirTemp(".", "aysort-"))
	}

	defer func() { throw(os.RemoveAll(tmpDir)) }()

	mergeChunks(spillChunks(in, chunkBytes, tmpDir), out)

	return 0
}

func spillChunks(in io.Reader, chunkBytes int, tmpDir string) []string {
	r := bufio.NewReaderSize(in, 1<<20)

	var chunks []string
	var lines []string

	size := 0

	spill := func() {
		if len(lines) == 0 {
			return
		}

		sort.Strings(lines)

		path := filepath.Join(tmpDir, "chunk-"+strconv.Itoa(len(chunks)))
		f := throw2(os.Create(path))
		bw := bufio.NewWriterSize(f, 1<<20)

		for _, ln := range lines {
			throw2(bw.WriteString(ln))
		}

		throw(bw.Flush())
		throw(f.Close())

		chunks = append(chunks, path)
		lines = lines[:0]
		size = 0
	}

	for {
		line, err := r.ReadString('\n')

		if len(line) > 0 {
			lines = append(lines, line)
			size += len(line)

			if size >= chunkBytes {
				spill()
			}
		}

		if err == io.EOF {
			break
		}

		throw(err)
	}

	spill()

	return chunks
}

type mergeItem struct {
	line   string
	reader *bufio.Reader
	closer io.Closer
}

type mergeHeap []*mergeItem

func (h mergeHeap) Len() int {
	return len(h)
}

func (h mergeHeap) Less(i, j int) bool {
	return h[i].line < h[j].line
}

func (h mergeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *mergeHeap) Push(x any) {
	*h = append(*h, x.(*mergeItem))
}

func (h *mergeHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]

	*h = old[:n-1]

	return it
}

func mergeChunks(chunks []string, out io.Writer) {
	bw := bufio.NewWriterSize(out, 1<<20)

	defer func() { throw(bw.Flush()) }()

	h := &mergeHeap{}

	heap.Init(h)

	for _, path := range chunks {
		f := throw2(os.Open(path))
		r := bufio.NewReaderSize(f, 1<<20)
		line, err := r.ReadString('\n')

		if err != nil && err != io.EOF {
			throw(err)
		}

		heap.Push(h, &mergeItem{line: line, reader: r, closer: f})
	}

	for h.Len() > 0 {
		it := heap.Pop(h).(*mergeItem)

		throw2(bw.WriteString(it.line))

		next, err := it.reader.ReadString('\n')

		if err != nil && err != io.EOF {
			throw(err)
		}

		if next != "" {
			it.line = next
			heap.Push(h, it)
		} else {
			throw(it.closer.Close())
		}
	}
}
