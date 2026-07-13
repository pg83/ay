package main

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"unsafe"
)

const (
	direntBlock       = 1 << 16
	atSymlinkNofollow = 0x100
)

func (fs *OsFS) platformInit() {
	for {
		fd, err := syscall.Open(fs.srcRoot, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY, 0)

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			throwFmt("open source root %s: %v", fs.srcRoot, err)
		}

		fs.rootFD = fd

		return
	}
}

func (fs *OsFS) openatRel(rel string, flags int) (int, syscall.Errno) {
	if rel == "" {
		rel = "."
	}

	p := append(fs.pathBuf[:0], rel...)

	p = append(p, 0)
	fs.pathBuf = p

	for {
		r1, _, errno := syscall.Syscall6(syscall.SYS_OPENAT, uintptr(fs.rootFD), uintptr(unsafe.Pointer(&p[0])), uintptr(flags), 0, 0, 0)

		if errno == syscall.EINTR {
			continue
		}

		return int(r1), errno
	}
}

func (fs *OsFS) readFileRel(rel string) [][]byte {
	if fs.mmapCur != nil {
		throw(syscall.Munmap(fs.mmapCur))

		fs.mmapCur = nil
	}

	fd, errno := fs.openatRel(rel, syscall.O_RDONLY|syscall.O_CLOEXEC)

	if errno != 0 {
		throwFmt("open %s: %v", rel, errno)
	}

	defer syscall.Close(fd)

	n := readEINTR(fd, fs.readBuf)
	fs.readResult[0] = fs.readBuf[:n]

	if n < len(fs.readBuf) {
		return fs.readResult[:1]
	}

	var st syscall.Stat_t

	throw(syscall.Fstat(fd, &st))

	tailSize := int(st.Size) - n

	if tailSize <= 0 {
		return fs.readResult[:1]
	}

	fs.mmapCur = throw2(syscall.Mmap(fd, int64(n), tailSize, syscall.PROT_READ, syscall.MAP_PRIVATE|syscall.MAP_POPULATE))
	fs.readResult[1] = fs.mmapCur

	return fs.readResult[:2]
}

func readEINTR(fd int, p []byte) int {
	for {
		n, err := syscall.Read(fd, p)

		if err == syscall.EINTR {
			continue
		}

		throw(err)

		return n
	}
}

func (fs *OsFS) fstatatRel(rel string, st *syscall.Stat_t) bool {
	p := append(fs.pathBuf[:0], rel...)

	p = append(p, 0)
	fs.pathBuf = p

	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_NEWFSTATAT, uintptr(fs.rootFD), uintptr(unsafe.Pointer(&p[0])), uintptr(unsafe.Pointer(st)), uintptr(atSymlinkNofollow), 0, 0)

		if errno == syscall.EINTR {
			continue
		}

		return errno == 0
	}
}

func (fs *OsFS) readDirAll(rel string, buf *[]byte) (int, bool) {
	fd, errno := fs.openatRel(rel, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY)

	if errno != 0 {
		return 0, false
	}

	defer syscall.Close(fd)

	total := 0

	for {
		if total+direntBlock > len(*buf) {
			grown := 2 * len(*buf)

			if grown < total+direntBlock {
				grown = total + direntBlock
			}

			next := make([]byte, grown)

			copy(next, (*buf)[:total])
			*buf = next
		}

		n, err := syscall.Getdents(fd, (*buf)[total:])

		for err == syscall.EINTR {
			n, err = syscall.Getdents(fd, (*buf)[total:])
		}

		if err != nil {
			return 0, false
		}

		if n == 0 {
			return total, true
		}

		total += n
	}
}

func (fs *OsFS) readDirViewRel(dir STR, rel string) DirView {
	n, ok := fs.readDirAll(rel, &fs.direntBuf)

	if !ok {
		return DirView{}
	}

	ents := fs.direntBuf[:n]
	// A linux_dirent64 record is at least 24 bytes including alignment.  The
	// arena only commits the entries actually written, so this upper bound lets
	// us decode and intern each name in one pass.
	block := fs.dirNames.alloc(len(ents) / 24)
	k := 0

	for off := 0; off < len(ents); {
		reclen := int(binary.LittleEndian.Uint16(ents[off+16:]))
		typ := ents[off+18]
		name := ents[off+19 : off+reclen]

		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}

		off += reclen

		if len(name) == 1 && name[0] == '.' || len(name) == 2 && name[0] == '.' && name[1] == '.' {
			continue
		}

		isDir := typ == syscall.DT_DIR

		if typ == syscall.DT_UNKNOWN {
			var st syscall.Stat_t

			if fs.fstatatRel(joinRel(rel, string(name)), &st) {
				isDir = st.Mode&syscall.S_IFMT == syscall.S_IFDIR
			}
		}

		id := internBytes(name)
		packed := uint32(id) << 1

		if isDir {
			packed |= 1
		}

		block[k] = packed
		k++
		fs.dirEntries.put(splitMix64(uint32(dir), uint32(id)), isDir)
	}

	if k == 0 {
		return DirView{dir: dir, names: emptyDirNames}
	}

	fs.dirNames.commit(k)

	return DirView{dir: dir, names: block[:k]}
}
