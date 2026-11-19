//go:build !aygate

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const (
	dirFDCacheSize      = 768
	fdCloseBatchSize    = 32
	fdCloseQueueBatches = 8
)

type dirFDCacheEntry struct {
	rel string
	fd  int
}

type fsBackend struct {
	testReadErrors map[string]error
	readBuf        []byte
	fileCloseQueue chan fdCloseBatch
	closePending   fdCloseBatch
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
	fs.fileCloseQueue = make(chan fdCloseBatch, fdCloseQueueBatches)
	fs.dirFD = -1
	fs.dirFDs = make(map[string]int, dirFDCacheSize)

	fs.platformInit()

	go fs.closeFiles()
}

type fdCloseBatch struct {
	n   int
	fds [fdCloseBatchSize]int32
}

func (fs *OsFS) closeFiles() {
	for batch := range fs.fileCloseQueue {
		for i := 0; i < batch.n; i++ {
			syscall.Close(int(batch.fds[i]))
		}
	}
}

func (fs *OsFS) closeFile(fd int) {
	batch := &fs.closePending

	batch.fds[batch.n] = int32(fd)
	batch.n++

	if batch.n == len(batch.fds) {
		fs.fileCloseQueue <- *batch
		batch.n = 0
	}
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
