//go:build !aygate

package main

import (
	"os"
	"strings"
	"syscall"
)

func (fs *OsFS) testPathReadError(rel string) error {
	if err, cached := fs.testReadErrors[rel]; cached {
		return err
	}

	roots := []string{rel}

	if info, err := os.Stat(fs.rootSlash + rel); err == nil && info.IsDir() {
		roots = nil

		for _, packed := range fs.listdirRel(rel).names {
			roots = append(roots, joinRel(rel, STR(packed>>1).string()))
		}
	}

	var readErr error

	for _, root := range roots {
		fs.walk(root, func(name string, isDir bool) bool {
			if readErr != nil {
				return false
			}

			if isDir {
				base := baseName(name)

				return base != ".svn" && base != ".cache" && base != ".idea"
			}

			if strings.HasSuffix(name, ".pyc") {
				return false
			}

			file := fs.rootSlash + name
			info, err := os.Lstat(file)

			if err == nil && info.Mode().IsRegular() {
				var cached bool

				readErr, cached = fs.testReadErrors[name]

				if !cached {
					readErr = fs.readTestPathFile(file)
					fs.testReadErrors[name] = readErr
				}
			}

			return false
		})
	}

	fs.testReadErrors[rel] = readErr

	return readErr
}

func (fs *OsFS) readTestPathFile(name string) error {
	fd, err := syscall.Open(name, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)

	if err != nil {
		return &os.PathError{Op: "open", Path: name, Err: err}
	}

	defer fs.closeFile(fd)

	for {
		n, err := chaosTestDataRead(fd, fs.readBuf)

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
