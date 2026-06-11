package main

import (
	"bufio"
	"container/heap"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const defaultSortChunkBytes = 256 << 20

func cmdDumpSort(args []string) int {
	var inPath, outPath string
	chunkBytes := defaultSortChunkBytes

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
			chunkBytes = int(Throw2(strconv.Atoi(arg(args, i))))
		default:
			ThrowFmt("dump sort: unknown argument %q", args[i])
		}
	}

	var in io.Reader

	if inPath == "" || inPath == "-" {
		in = os.Stdin
	} else {
		f := Throw2(os.Open(inPath))

		defer func() { Throw(f.Close()) }()

		in = f
	}

	var out io.Writer

	if outPath == "" || outPath == "-" {
		out = os.Stdout
	} else {
		f := Throw2(os.Create(outPath))

		defer func() { Throw(f.Close()) }()

		out = f
	}

	tmpBase := "."

	if outPath != "" && outPath != "-" {
		tmpBase = filepath.Dir(outPath)
	}

	tmpDir, err := os.MkdirTemp(tmpBase, "aysort-")

	if err != nil {
		tmpDir = Throw2(os.MkdirTemp(".", "aysort-"))
	}

	defer func() { Throw(os.RemoveAll(tmpDir)) }()

	chunks := spillChunks(in, chunkBytes, tmpDir)
	mergeChunks(chunks, out)

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
		f := Throw2(os.Create(path))
		bw := bufio.NewWriterSize(f, 1<<20)

		for _, ln := range lines {
			Throw2(bw.WriteString(ln))
		}

		Throw(bw.Flush())
		Throw(f.Close())

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

		Throw(err)
	}

	spill()

	return chunks
}

type MergeItem struct {
	line   string
	reader *bufio.Reader
	closer io.Closer
}

type MergeHeap []*MergeItem

func (h MergeHeap) len() int {
	return len(h)
}

// Len implements container/heap.Interface (its sort.Interface part).
func (h MergeHeap) Len() int {
	return h.len()
}

func (h MergeHeap) less(i, j int) bool {
	return h[i].line < h[j].line
}

// Less implements container/heap.Interface (its sort.Interface part).
func (h MergeHeap) Less(i, j int) bool {
	return h.less(i, j)
}

func (h MergeHeap) swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Swap implements container/heap.Interface (its sort.Interface part).
func (h MergeHeap) Swap(i, j int) {
	h.swap(i, j)
}

func (h *MergeHeap) push(x any) {
	*h = append(*h, x.(*MergeItem))
}

// Push implements container/heap.Interface; the heap machinery finds it by name.
func (h *MergeHeap) Push(x any) {
	h.push(x)
}

func (h *MergeHeap) pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

// Pop implements container/heap.Interface; the heap machinery finds it by name.
func (h *MergeHeap) Pop() any {
	return h.pop()
}

func mergeChunks(chunks []string, out io.Writer) {
	bw := bufio.NewWriterSize(out, 1<<20)

	defer func() { Throw(bw.Flush()) }()

	h := &MergeHeap{}
	heap.Init(h)

	for _, path := range chunks {
		f := Throw2(os.Open(path))
		r := bufio.NewReaderSize(f, 1<<20)
		line, err := r.ReadString('\n')

		if err != nil && err != io.EOF {
			Throw(err)
		}

		if line == "" && err == io.EOF {
			Throw(f.Close())
			continue
		}

		heap.Push(h, &MergeItem{line: line, reader: r, closer: f})
	}

	for h.len() > 0 {
		it := heap.Pop(h).(*MergeItem)
		Throw2(bw.WriteString(it.line))

		next, err := it.reader.ReadString('\n')

		if err != nil && err != io.EOF {
			Throw(err)
		}

		if next != "" {
			it.line = next
			heap.Push(h, it)
		} else {
			Throw(it.closer.Close())
		}
	}
}
