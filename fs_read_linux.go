package main

import (
	"bytes"
	"encoding/binary"
	"syscall"
)

// readFileInto reads path into buf's storage (growing it as needed) and returns
// the filled slice. Raw open/fstat/read/close instead of os.Open + (*os.File).Read:
// no per-read *os.File heap object and finalizer, no poll.FD indirection — that
// wrapper overhead was ~3% of gen CPU over sg5's ~45k reads. EINTR is retried
// here (the os layer used to do it for us; raw reads can see it under Go's async
// preemption on some filesystems).
func readFileInto(path string, buf []byte) []byte {
	fd := openEINTR(path)

	defer syscall.Close(fd)

	buf = buf[:0]

	// Fstat into a stack Stat_t instead of an os.FileInfo — (*os.File).Stat()
	// heap-allocates an *os.fileStat per read (~10MB churn over a run). A raw
	// read at EOF returns n=0 with no error, so n==0 is the EOF condition below
	// (the fstat-sized loop also stops there if the file shrank mid-read).
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

func openEINTR(path string) int {
	for {
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			throwFmt("open %s: %v", path, err)
		}

		return fd
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

// direntBlock is the reused getdents64 buffer size: large enough that most
// directories list in one syscall.
const direntBlock = 1 << 16

// readDirEntries streams (name, isDir) for every entry of the directory at
// full via raw getdents64 into the caller's reused buffer — one block read,
// names carved out as byte slices: no per-entry os.DirEntry object, no
// []DirEntry, no sort. name is valid only during the visit call. Returns
// false when full cannot be listed (mirrors os.ReadDir's error → nil).
func readDirEntries(full string, buf []byte, visit func(name []byte, isDir bool)) bool {
	fd, err := syscall.Open(full, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY, 0)

	for err == syscall.EINTR {
		fd, err = syscall.Open(full, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY, 0)
	}

	if err != nil {
		return false
	}

	defer syscall.Close(fd)

	for {
		n, err := syscall.Getdents(fd, buf)

		for err == syscall.EINTR {
			n, err = syscall.Getdents(fd, buf)
		}

		if err != nil {
			return false
		}

		if n == 0 {
			return true
		}

		// linux_dirent64: ino u64, off u64, reclen u16, type u8, NUL-terminated name.
		for off := 0; off < n; {
			reclen := int(binary.LittleEndian.Uint16(buf[off+16:]))
			typ := buf[off+18]
			name := buf[off+19 : off+reclen]

			if i := bytes.IndexByte(name, 0); i >= 0 {
				name = name[:i]
			}

			if !(len(name) == 1 && name[0] == '.') && !(len(name) == 2 && name[0] == '.' && name[1] == '.') {
				isDir := typ == syscall.DT_DIR

				if typ == syscall.DT_UNKNOWN {
					// Filesystems without d_type (rare): fall back to lstat,
					// like os's dirent layer.
					var st syscall.Stat_t

					if syscall.Lstat(full+"/"+string(name), &st) == nil {
						isDir = st.Mode&syscall.S_IFMT == syscall.S_IFDIR
					}
				}

				visit(name, isDir)
			}

			off += reclen
		}
	}
}
