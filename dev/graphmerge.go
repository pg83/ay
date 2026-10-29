//go:build ignore

package main

import (
	"bufio"
	"container/heap"
	"fmt"
	"io"
	"os"
)

const usage = `usage: graphmerge <graph.norm.jsonl>... > merged.norm.jsonl
  the sorted union of sorted normalized graphs: one line per distinct node
`

const batchBytes = 4 << 20

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "graphmerge: "+format+"\n", args...)
	os.Exit(1)
}

type batch struct {
	lines []string
	err   error
}

func read(path string, out chan<- batch) {
	defer close(out)

	f, err := os.Open(path)

	if err != nil {
		out <- batch{err: err}

		return
	}

	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)

	var lines []string

	size := 0
	previous := ""

	for {
		line, err := r.ReadString('\n')

		if len(line) > 0 {
			if line[len(line)-1] != '\n' {
				line += "\n"
			}

			if line < previous {
				out <- batch{err: fmt.Errorf("%s: input is not sorted", path)}

				return
			}

			previous = line
			lines = append(lines, line)
			size += len(line)

			if size >= batchBytes {
				out <- batch{lines: lines}
				lines, size = nil, 0
			}
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			out <- batch{err: fmt.Errorf("%s: %w", path, err)}

			return
		}
	}

	if len(lines) > 0 {
		out <- batch{lines: lines}
	}
}

type cursor struct {
	in    <-chan batch
	lines []string
}

func (c *cursor) next() bool {
	if len(c.lines) > 1 {
		c.lines = c.lines[1:]

		return true
	}

	for b := range c.in {
		if b.err != nil {
			die("%v", b.err)
		}

		if len(b.lines) > 0 {
			c.lines = b.lines

			return true
		}
	}

	return false
}

type cursors []*cursor

func (h cursors) Len() int           { return len(h) }
func (h cursors) Less(i, j int) bool { return h[i].lines[0] < h[j].lines[0] }
func (h cursors) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *cursors) Push(x any)        { *h = append(*h, x.(*cursor)) }

func (h *cursors) Pop() any {
	old := *h
	c := old[len(old)-1]

	*h = old[:len(old)-1]

	return c
}

func main() {
	paths := os.Args[1:]

	if len(paths) == 0 || paths[0] == "-h" || paths[0] == "--help" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	h := cursors{}

	for _, path := range paths {
		in := make(chan batch, 4)

		go read(path, in)

		h = append(h, &cursor{in: in})
	}

	live := h[:0]

	for _, c := range h {
		c.lines = []string{""}

		if c.next() {
			live = append(live, c)
		}
	}

	h = live

	heap.Init(&h)

	out := bufio.NewWriterSize(os.Stdout, 1<<20)
	last := ""

	for h.Len() > 0 {
		c := h[0]

		if line := c.lines[0]; line != last {
			if _, err := out.WriteString(line); err != nil {
				die("%v", err)
			}

			last = line
		}

		if c.next() {
			heap.Fix(&h, 0)
		} else {
			heap.Pop(&h)
		}
	}

	if err := out.Flush(); err != nil {
		die("%v", err)
	}
}
