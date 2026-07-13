//go:build !linux

package main

import (
	"io"
	"os"
)

func (fs *OsFS) platformInit() {
}

func (fs *OsFS) readFileRel(rel string) [][]byte {
	f := throw2(os.Open(fs.rootSlash + rel))

	defer f.Close()

	n, err := io.ReadFull(f, fs.readBuf)

	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		throw(err)
	}

	fs.readResult[0] = fs.readBuf[:n]

	if n < len(fs.readBuf) {
		return fs.readResult[:1]
	}

	info := throw2(f.Stat())
	tailSize := int(info.Size()) - n

	if tailSize <= 0 {
		return fs.readResult[:1]
	}

	tail := make([]byte, tailSize)
	_, err = io.ReadFull(f, tail)
	throw(err)
	fs.readResult[1] = tail

	return fs.readResult[:2]
}

func (fs *OsFS) readDirViewRel(dir STR, rel string) DirView {
	full := fs.rootSlash + rel

	if rel == "" {
		full = fs.srcRoot
	}

	entries, err := os.ReadDir(full)

	if err != nil {
		return DirView{}
	}

	if len(entries) == 0 {
		return DirView{dir: dir, names: emptyDirNames}
	}

	block := fs.dirNames.alloc(len(entries))

	for i, e := range entries {
		id := internStr(e.Name())
		packed := uint32(id) << 1

		if e.IsDir() {
			packed |= 1
		}

		block[i] = packed
		fs.dirEntries.put(uint32(dir), uint32(id), e.IsDir())
	}

	fs.dirNames.commit(len(entries))

	return DirView{dir: dir, names: block[:len(entries)]}
}
