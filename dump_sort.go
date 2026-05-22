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

const defaultSortChunkBytes = 256 << 20 // 256 MiB per in-memory chunk

// cmdDumpSort is a generic external-merge line sorter: read lines, sort
// fixed-size chunks in memory and spill each to a temp file, then k-way
// merge the sorted chunks with a min-heap. Memory is bounded by the chunk
// size plus one buffered reader per chunk — it never holds the whole input.
// Comparison is bytewise (LC_ALL=C), matching Go string ordering.
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
		// Output dir may be non-writable (e.g. --out /dev/null); fall back
		// to the current directory.
		tmpDir = Throw2(os.MkdirTemp(".", "aysort-"))
	}
	defer func() { Throw(os.RemoveAll(tmpDir)) }()

	chunks := spillChunks(in, chunkBytes, tmpDir)
	mergeChunks(chunks, out)

	return 0
}

// spillChunks reads all lines, sorting and writing each ~chunkBytes batch to
// a temp file. Returns the chunk file paths. Each retained line keeps its
// trailing newline so the merge reconstructs the stream verbatim.
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

type mergeItem struct {
	line   string
	reader *bufio.Reader
	closer io.Closer
}

type mergeHeap []*mergeItem

func (h mergeHeap) Len() int            { return len(h) }
func (h mergeHeap) Less(i, j int) bool  { return h[i].line < h[j].line }
func (h mergeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(x any)         { *h = append(*h, x.(*mergeItem)) }
func (h *mergeHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

// mergeChunks k-way merges the sorted chunk files into out via a min-heap,
// holding one buffered reader and one pending line per chunk.
func mergeChunks(chunks []string, out io.Writer) {
	bw := bufio.NewWriterSize(out, 1<<20)
	defer func() { Throw(bw.Flush()) }()

	h := &mergeHeap{}
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
		heap.Push(h, &mergeItem{line: line, reader: r, closer: f})
	}

	for h.Len() > 0 {
		it := heap.Pop(h).(*mergeItem)
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
