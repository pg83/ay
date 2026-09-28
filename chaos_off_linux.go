//go:build !aychaos

package main

import "syscall"

func chaosOpen(path string, mode int, perm uint32) (int, error) {
	return syscall.Open(path, mode, perm)
}

func chaosOpenat(dirFD int, path *byte, flags int) (int, syscall.Errno) {
	return openatRaw(dirFD, path, flags)
}

func chaosRead(fd int, p []byte) (int, error) {
	return syscall.Read(fd, p)
}

func chaosFstatat(dirFD int, path *byte, st *syscall.Stat_t) syscall.Errno {
	return fstatatRaw(dirFD, path, st)
}

func chaosGetdents(fd int, buf []byte) (int, error) {
	return syscall.Getdents(fd, buf)
}

func chaosDirentType(typ byte) byte {
	return typ
}
