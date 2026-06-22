//go:build !linux

package main

import (
	"io"
	"os"
)

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

func (fs *OsFS) platformInit() {
}

func (fs *OsFS) readFileRel(rel string, buf []byte) []byte {
	return readFileInto(fs.rootSlash+rel, buf)
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
		fs.dirEntries.put(splitMix64(uint32(dir), uint32(id)), e.IsDir())
	}

	fs.dirNames.commit(len(entries))

	return DirView{dir: dir, names: block[:len(entries):len(entries)]}
}
