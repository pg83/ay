package main

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"unsafe"
)

// platformInit pins the source root as rootFD; reads/listdirs resolve via openat.
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

// openatRel opens rel under rootFD — the NUL-terminated rel is assembled in the
// reused scratch, so the open allocates nothing.
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

// readFileRel reads rel into buf (growing as needed) and returns the filled slice.
// Raw openat/fstat/read/close avoids the *os.File heap object, finalizer and poll.FD
// indirection (~3% of gen CPU). EINTR is retried here.
func (fs *OsFS) readFileRel(rel string, buf []byte) []byte {
	fd, errno := fs.openatRel(rel, syscall.O_RDONLY|syscall.O_CLOEXEC)

	if errno != 0 {
		throwFmt("open %s: %v", rel, errno)
	}

	defer syscall.Close(fd)

	buf = buf[:0]

	// Fstat into a stack Stat_t instead of an os.FileInfo (which heap-allocates per
	// read). A raw read at EOF returns n=0, so n==0 is the EOF condition below.
	var st syscall.Stat_t

	if statErr := syscall.Fstat(fd, &st); statErr == nil {
		sz := int(st.Size)

		if sz > cap(buf) {
			buf = make([]byte, 0, sz)
		}

		for len(buf) < sz {
			n := readEINTR(fd, buf[len(buf):sz])

			if n == 0 {
				return buf
			}

			buf = buf[:len(buf)+n]
		}

		return buf
	}

	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}

		n := readEINTR(fd, buf[len(buf):cap(buf)])

		if n == 0 {
			return buf
		}

		buf = buf[:len(buf)+n]
	}
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

// direntBlock is the reused getdents64 buffer size: large enough for most
// directories in one syscall.
const direntBlock = 1 << 16

// atSymlinkNofollow is AT_SYMLINK_NOFOLLOW (not exported by syscall).
const atSymlinkNofollow = 0x100

// fstatatRel is lstat relative to rootFD (SYS_NEWFSTATAT; no Fstatat exported).
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

// readDirAll reads the whole getdents64 stream of the directory at rel into *buf
// (grown and reused) and returns the filled length. false means "not listable".
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

// readDirViewRel lists the directory at rel into the shared name store: one raw
// getdents64 stream, parsed twice — count (for an exactly-sized arena reservation),
// then fill. Each name interns from the dirent bytes and lands packed (STR<<1|isDir);
// membership goes into dirEntries under splitMix64(dir, name). A zero view means
// "not listable".
func (fs *OsFS) readDirViewRel(dir STR, rel string) DirView {
	n, ok := fs.readDirAll(rel, &fs.direntBuf)

	if !ok {
		return DirView{}
	}

	ents := fs.direntBuf[:n]
	count := 0
	forEachDirent(ents, func([]byte, byte) {
		count++
	})

	if count == 0 {
		return DirView{dir: dir, names: emptyDirNames}
	}

	block := fs.dirNames.alloc(count)
	k := 0
	forEachDirent(ents, func(name []byte, typ byte) {
		isDir := typ == syscall.DT_DIR

		if typ == syscall.DT_UNKNOWN {
			// Filesystems without d_type (rare): lstat.
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
	})
	fs.dirNames.commit(k)

	return DirView{dir: dir, names: block[:k:k]}
}

// forEachDirent walks the raw linux_dirent64 records in b, skipping "." and "..".
// The name slice is a view into b.
func forEachDirent(b []byte, visit func(name []byte, typ byte)) {
	for off := 0; off < len(b); {
		reclen := int(binary.LittleEndian.Uint16(b[off+16:]))
		typ := b[off+18]
		name := b[off+19 : off+reclen]

		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}

		if !(len(name) == 1 && name[0] == '.') && !(len(name) == 2 && name[0] == '.' && name[1] == '.') {
			visit(name, typ)
		}

		off += reclen
	}
}
