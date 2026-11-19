package main

import "strings"

func (fs *OsFS) testPathReadError(rel string) error {
	if err, cached := fs.testReadErrors[rel]; cached {
		return err
	}

	roots := []string{rel}

	if present, isDir := fs.existsRel(rel); present && isDir {
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

			var cached bool

			readErr, cached = fs.testReadErrors[name]

			if !cached {
				readErr = fs.backend.readError(name)
				fs.testReadErrors[strings.Clone(name)] = readErr
			}

			return false
		})
	}

	fs.testReadErrors[rel] = readErr

	return readErr
}
