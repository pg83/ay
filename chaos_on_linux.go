//go:build aychaos

package main

import "syscall"

// Linux syscalls behind the seam. An interrupted call fails with EINTR
// before it runs, as a signal arriving first makes it do.
var (
	chaosEINTROpen     = newChaosFault("eintr-open")
	chaosEINTROpenat   = newChaosFault("eintr-openat")
	chaosEINTRRead     = newChaosFault("eintr-read")
	chaosEINTRFstatat  = newChaosFault("eintr-fstatat")
	chaosEINTRGetdents = newChaosFault("eintr-getdents")
	chaosGetdentsEIO   = newChaosFault("getdents-eio")
	chaosDirentUnknown = newChaosNumber("dirent-unknown")
)

func chaosOpen(path string, mode int, perm uint32) (int, error) {
	if chaosEINTROpen.fire() {
		return -1, syscall.EINTR
	}

	return syscall.Open(path, mode, perm)
}

func chaosOpenat(dirFD int, path *byte, flags int) (int, syscall.Errno) {
	if chaosEINTROpenat.fire() {
		return -1, syscall.EINTR
	}

	return openatRaw(dirFD, path, flags)
}

func chaosRead(fd int, p []byte) (int, error) {
	if chaosEINTRRead.fire() {
		return 0, syscall.EINTR
	}

	return syscall.Read(fd, p)
}

func chaosFstatat(dirFD int, path *byte, st *syscall.Stat_t) syscall.Errno {
	if chaosEINTRFstatat.fire() {
		return syscall.EINTR
	}

	return fstatatRaw(dirFD, path, st)
}

// chaosGetdents interrupts a listing or fails it with EIO, as a failing disk
// or a FUSE server does.
func chaosGetdents(fd int, buf []byte) (int, error) {
	if chaosEINTRGetdents.fire() {
		return 0, syscall.EINTR
	}

	if chaosGetdentsEIO.fire() {
		return 0, syscall.EIO
	}

	return syscall.Getdents(fd, buf)
}

// chaosDirentType reports every entry's type as unknown while dirent-unknown
// is set to 1, as filesystems without d_type do.
func chaosDirentType(typ byte) byte {
	if chaosDirentUnknown.number(0) == 1 {
		return syscall.DT_UNKNOWN
	}

	return typ
}
