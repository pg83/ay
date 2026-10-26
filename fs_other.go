//go:build !linux && !aygate

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
	k := 0

	for _, e := range entries {
		isDir := e.IsDir()

		if e.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(full + "/" + e.Name())

			if err != nil {
				continue
			}

			isDir = info.IsDir()
		}

		id := internStr(e.Name())
		packed := uint32(id) << 1

		if isDir {
			packed |= 1
		}

		block[k] = packed
		k++
		fs.dirEntries.put(splitMix64(uint32(dir), uint32(id)), isDir)
	}

	fs.dirNames.commit(k)

	return DirView{dir: dir, names: block[:k]}
}
