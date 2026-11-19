//go:build !linux

package main

import (
	"errors"
	"io"
	"os"
	"syscall"
)

func (b *DirBackend) platformInit() {
}

func osErrno(err error) syscall.Errno {
	var errno syscall.Errno

	if errors.As(err, &errno) {
		return errno
	}

	return syscall.EIO
}

func (b *DirBackend) readFile(rel string) ([][]byte, syscall.Errno) {
	f, err := os.Open(b.rootSlash + rel)

	if err != nil {
		return nil, osErrno(err)
	}

	defer f.Close()

	n, err := io.ReadFull(f, b.readBuf)

	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		throw(err)
	}

	b.readResult[0] = b.readBuf[:n]

	if n < len(b.readBuf) {
		return b.readResult[:1], 0
	}

	info := throw2(f.Stat())
	tailSize := int(info.Size()) - n

	if tailSize <= 0 {
		return b.readResult[:1], 0
	}

	tail := make([]byte, tailSize)

	_, err = io.ReadFull(f, tail)
	throw(err)
	b.readResult[1] = tail

	return b.readResult[:2], 0
}

func (b *DirBackend) readDir(rel string, sink DirSink) syscall.Errno {
	full := b.rootSlash + rel

	if rel == "" {
		full = b.root
	}

	entries, err := os.ReadDir(full)

	if err != nil {
		return osErrno(err)
	}

	sink.dirBegin(len(entries))

	for _, e := range entries {
		isDir := e.IsDir()

		if e.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(full + "/" + e.Name())

			if err != nil {
				continue
			}

			isDir = info.IsDir()
		}

		sink.dirEntry([]byte(e.Name()), isDir)
	}

	return 0
}
