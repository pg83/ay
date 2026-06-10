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
	f := Throw2(os.Open(path))
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

				Throw(err)
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

			Throw(err)
		}
	}
}
