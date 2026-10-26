//go:build !aygate

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const dirFDCacheSize = 768

type dirFDCacheEntry struct {
	rel string
	fd  int
}

type fsBackend struct {
	testReadErrors map[string]error
	readBuf        []byte
	fileCloseQueue chan int
	direntBuf      []byte
	mmapCur        []byte
	rootFD         int
	dirFD          int
	dirFDRel       string
	dirFDs         map[string]int
	dirFDRing      [dirFDCacheSize]dirFDCacheEntry
	dirFDNext      int
	pathBuf        []byte
}

func (fs *OsFS) initBackend() {
	fs.readBuf = make([]byte, readChunkSize)
	fs.testReadErrors = map[string]error{}
	fs.fileCloseQueue = make(chan int, 256)
	fs.dirFD = -1
	fs.dirFDs = make(map[string]int, dirFDCacheSize)

	fs.platformInit()

	go fs.closeFiles()
}

func (fs *OsFS) closeFiles() {
	for fd := range fs.fileCloseQueue {
		syscall.Close(fd)
	}
}

func (fs *OsFS) closeFile(fd int) {
	fs.fileCloseQueue <- fd
}

func (fs *OsFS) sourcePathExists(rel string) bool {
	_, err := os.Stat(filepath.Join(fs.srcRoot, rel))

	if err != nil && !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ENOTDIR) &&
		!errors.Is(err, syscall.ENAMETOOLONG) && !errors.Is(err, syscall.EACCES) && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ELOOP) {
		throw(err)
	}

	return err == nil
}

func (fs *OsFS) confRoot() string {
	return fs.srcRoot
}
