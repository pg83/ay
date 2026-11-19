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

type DirBackend struct {
	root           string
	rootSlash      string
	readBuf        []byte
	readResult     [2][]byte
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

func newDirBackend(root string) *DirBackend {
	b := &DirBackend{
		root:           root,
		rootSlash:      root + "/",
		readBuf:        make([]byte, readChunkSize),
		fileCloseQueue: make(chan fdCloseBatch, fdCloseQueueBatches),
		dirFD:          -1,
		dirFDs:         make(map[string]int, dirFDCacheSize),
	}

	b.platformInit()

	go b.closeFiles()

	return b
}

type fdCloseBatch struct {
	n   int
	fds [fdCloseBatchSize]int32
}

func (b *DirBackend) closeFiles() {
	for batch := range b.fileCloseQueue {
		for i := 0; i < batch.n; i++ {
			syscall.Close(int(batch.fds[i]))
		}
	}
}

func (b *DirBackend) closeFile(fd int) {
	batch := &b.closePending

	batch.fds[batch.n] = int32(fd)
	batch.n++

	if batch.n == len(batch.fds) {
		b.fileCloseQueue <- *batch
		batch.n = 0
	}
}

func (b *DirBackend) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(b.root, rel))

	if err != nil && !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ENOTDIR) &&
		!errors.Is(err, syscall.ENAMETOOLONG) && !errors.Is(err, syscall.EACCES) && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ELOOP) {
		throw(err)
	}

	return err == nil
}

func (b *DirBackend) readError(rel string) error {
	name := b.rootSlash + rel

	if info, err := os.Lstat(name); err != nil || !info.Mode().IsRegular() {
		return nil
	}

	fd, err := syscall.Open(name, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)

	if err != nil {
		return &os.PathError{Op: "open", Path: name, Err: err}
	}

	defer b.closeFile(fd)

	for {
		n, err := chaosTestDataRead(fd, b.readBuf)

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			return &os.PathError{Op: "read", Path: name, Err: err}
		}

		if n == 0 {
			return nil
		}
	}
}

func (b *DirBackend) localRoot() string {
	return b.root
}
