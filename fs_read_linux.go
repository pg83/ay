package main

import "syscall"

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
