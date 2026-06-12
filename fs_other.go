//go:build !linux

package main

import (
	"io"
	"os"
)

// readFileInto is the portable fallback of the linux raw-syscall fast path (see
// fs_read_linux.go): os.Open + (*os.File).Read, sizing the buffer from f.Stat()
// when available so the common case is a single exact-size read.
func readFileInto(path string, buf []byte) []byte {
	f := throw2(os.Open(path))

	defer f.Close()

	buf = buf[:0]

	if info, err := f.Stat(); err == nil {
		sz := int(info.Size())

		if sz > cap(buf) {
			buf = make([]byte, 0, sz)
		}

		for len(buf) < sz {
			n, err := f.Read(buf[len(buf):sz])
			buf = buf[:len(buf)+n]

			if err != nil {
				if err == io.EOF {
					return buf
				}

				throw(err)
			}
		}

		return buf
	}

	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}

		n, err := f.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]

		if err != nil {
			if err == io.EOF {
				return buf
			}

			throw(err)
		}
	}
}

// platformInit is a no-op on the portable arm (no pinned root fd).
func (fs *OsFS) platformInit() {
}

// readFileRel is the portable twin of the linux openat fast path.
func (fs *OsFS) readFileRel(rel string, buf []byte) []byte {
	return readFileInto(fs.rootSlash+rel, buf)
}

// readDirMapRel is the portable fallback: os.ReadDir already returns the full,
// right-sized entry list.
func (fs *OsFS) readDirMapRel(rel string) (map[string]bool, bool) {
	full := fs.rootSlash + rel

	if rel == "" {
		full = fs.srcRoot
	}

	entries, err := os.ReadDir(full)

	if err != nil {
		return nil, false
	}

	out := make(map[string]bool, len(entries))

	for _, e := range entries {
		out[e.Name()] = e.IsDir()
	}

	return out, true
}
