//go:build ignore

package main

import (
	"archive/tar"
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const usage = `usage: graph < archive.tar > graph.json
  the reference graph JSON of an uncompressed resource tarball on stdin:
  graph.fuse.json at the top if present, otherwise the first *.json by path
`

const fuse = "graph.fuse.json"

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "graph: "+format+"\n", args...)
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

func regular(h *tar.Header) bool {
	switch h.Typeflag {
	case tar.TypeReg, tar.TypeCont, tar.TypeGNUSparse:
		return true
	}

	return false
}

func main() {
	if len(os.Args) > 1 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	r := tar.NewReader(bufio.NewReaderSize(os.Stdin, 1<<20))
	out := bufio.NewWriterSize(os.Stdout, 1<<20)

	var best string
	var spool *os.File

	found := false

	for {
		h, err := r.Next()

		if err == io.EOF {
			break
		}

		check(err)

		name := clean(h.Name)

		if found || !regular(h) || !strings.HasSuffix(name, ".json") {
			continue
		}

		if name == fuse {
			_, err = io.Copy(out, r)
			check(err)

			found = true

			continue
		}

		if best != "" && best <= name {
			continue
		}

		if spool == nil {
			spool, err = os.CreateTemp("", "ay-graph-")
			check(err)
			check(os.Remove(spool.Name()))
		} else {
			_, err = spool.Seek(0, io.SeekStart)
			check(err)
			check(spool.Truncate(0))
		}

		_, err = io.Copy(spool, r)
		check(err)

		best = name
	}

	if !found {
		if best == "" {
			die("no graph JSON in the archive")
		}

		_, err := spool.Seek(0, io.SeekStart)
		check(err)

		_, err = io.Copy(out, spool)
		check(err)
	}

	check(out.Flush())
}
