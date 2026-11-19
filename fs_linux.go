//go:build linux

package main

import (
	"bytes"
	"encoding/binary"
	"syscall"
)

const direntBlock = 1 << 16

func (b *DirBackend) platformInit() {
	for {
		fd, err := chaosOpen(b.root, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY, 0)

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			throwFmt("open source root %s: %v", b.root, err)
		}

		b.rootFD = fd

		return
	}
}

func (b *DirBackend) openatRel(dirFD int, rel string, flags int) (int, syscall.Errno) {
	if rel == "" {
		rel = "."
	}

	p := append(b.pathBuf[:0], rel...)

	p = append(p, 0)
	b.pathBuf = p

	for {
		fd, errno := chaosOpenat(dirFD, &p[0], flags)

		if errno == syscall.EINTR {
			continue
		}

		return fd, errno
	}
}

func (b *DirBackend) readFile(rel string) ([][]byte, syscall.Errno) {
	if b.mmapCur != nil {
		throw(syscall.Munmap(b.mmapCur))

		b.mmapCur = nil
	}

	dir, name := splitDirName(rel)
	dirFD, openRel := b.rootFD, rel

	if dir == b.dirFDRel && b.dirFD >= 0 {
		dirFD, openRel = b.dirFD, name
	} else if cached, ok := b.dirFDs[dir]; ok {
		dirFD, openRel = cached, name
	}

	fd, errno := b.openatRel(dirFD, openRel, syscall.O_RDONLY|syscall.O_CLOEXEC)

	if errno != 0 {
		return nil, errno
	}

	defer b.closeFile(fd)

	n := readEINTR(fd, b.readBuf)

	b.readResult[0] = b.readBuf[:n]

	if n < len(b.readBuf) {
		return b.readResult[:1], 0
	}

	var st syscall.Stat_t

	throw(syscall.Fstat(fd, &st))

	tailSize := int(st.Size) - n

	if tailSize <= 0 {
		return b.readResult[:1], 0
	}

	b.mmapCur = throw2(syscall.Mmap(fd, int64(n), tailSize, syscall.PROT_READ, syscall.MAP_PRIVATE|syscall.MAP_POPULATE))
	b.readResult[1] = b.mmapCur

	return b.readResult[:2], 0
}

func readEINTR(fd int, p []byte) int {
	for {
		n, err := chaosRead(fd, p)

		if err == syscall.EINTR {
			continue
		}

		throw(err)

		return n
	}
}

func (b *DirBackend) fstatatRel(rel string, st *syscall.Stat_t) bool {
	p := append(b.pathBuf[:0], rel...)

	p = append(p, 0)
	b.pathBuf = p

	for {
		errno := chaosFstatat(b.rootFD, &p[0], st)

		if errno == syscall.EINTR {
			continue
		}

		return errno == 0
	}
}

func (b *DirBackend) readDirAll(rel string, buf *[]byte) (int, syscall.Errno) {
	fd, errno := b.openatRel(b.rootFD, rel, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY)

	if errno != 0 {
		return 0, errno
	}

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

		n, err := chaosGetdents(fd, (*buf)[total:])

		for err == syscall.EINTR {
			n, err = chaosGetdents(fd, (*buf)[total:])
		}

		if err != nil {
			syscall.Close(fd)

			return 0, syscall.EIO
		}

		if n == 0 {
			slot := &b.dirFDRing[b.dirFDNext]

			if slot.fd > 0 {
				syscall.Close(slot.fd)
				delete(b.dirFDs, slot.rel)
			}

			*slot = dirFDCacheEntry{rel: rel, fd: fd}
			b.dirFDNext++

			if b.dirFDNext == dirFDCacheSize {
				b.dirFDNext = 0
			}

			b.dirFDs[rel] = fd
			b.dirFD = fd
			b.dirFDRel = rel

			return total, 0
		}

		total += n
	}
}

func (b *DirBackend) readDir(rel string, sink DirSink) syscall.Errno {
	n, errno := b.readDirAll(rel, &b.direntBuf)

	if errno != 0 {
		return errno
	}

	ents := b.direntBuf[:n]

	sink.dirBegin(len(ents) / 24)

	for off := 0; off < len(ents); {
		reclen := int(binary.LittleEndian.Uint16(ents[off+16:]))
		typ := chaosDirentType(ents[off+18])
		name := ents[off+19 : off+reclen]

		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}

		off += reclen

		if len(name) == 1 && name[0] == '.' || len(name) == 2 && name[0] == '.' && name[1] == '.' {
			continue
		}

		isDir := typ == syscall.DT_DIR

		if typ == syscall.DT_UNKNOWN || typ == syscall.DT_LNK {
			var st syscall.Stat_t

			if !b.fstatatRel(joinRel(rel, string(name)), &st) {
				continue
			}

			isDir = st.Mode&syscall.S_IFMT == syscall.S_IFDIR
		}

		sink.dirEntry(name, isDir)
	}

	return 0
}
