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

func cmdDumpSort(_ GlobalFlags, args []string) int {
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
			chunkBytes = int(throw2(strconv.Atoi(arg(args, i))))
		default:
			throwFmt("dump sort: unknown argument %q", args[i])
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

func mergeChunks(chunks []string, out io.Writer) {
	bw := bufio.NewWriterSize(out, 1<<20)

	defer func() { throw(bw.Flush()) }()

	h := &MergeHeap{}

	heap.Init(h)

	for _, path := range chunks {
		f := throw2(os.Open(path))
		r := bufio.NewReaderSize(f, 1<<20)
		line, err := r.ReadString('\n')

		if err != nil && err != io.EOF {
			throw(err)
		}

		if line == "" && err == io.EOF {
			throw(f.Close())

			continue
		}

		heap.Push(h, &MergeItem{line: line, reader: r, closer: f})
	}

	for h.len() > 0 {
		it := heap.Pop(h).(*MergeItem)

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
