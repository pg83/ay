package main

import (
	"io"
	"os"
	"path"
	"strings"
)

// FS is the source-tree filesystem facade. Production code drives an osFS
// (rooted at a real directory and cached lazily); tests drive a memFS built
// inline (testfs_test.go) so the suite does no disk I/O for fixture trees.
type FS interface {
	SourceRoot() string
	Listdir(rel string) map[string]bool
	Exists(rel string) (present bool, isDir bool)
	IsFile(rel string) bool
	IsDir(rel string) bool
	Read(rel string) []byte
	ReadInto(rel string, buf []byte) []byte
	ReadAbs(absPath string) []byte
	ExistsAbs(absPath string) (present bool, isDir bool)
	Walk(rel string, visit func(rel string, isDir bool))
	perfStats() fsPerfStats
}

type osFS struct {
	sourceRoot string
	rootSlash  string
	dirs       map[string]map[string]bool

	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
}

func NewFS(sourceRoot string) FS {
	return &osFS{
		sourceRoot: sourceRoot,
		rootSlash:  sourceRoot + "/",
		dirs:       make(map[string]map[string]bool, 1024),
	}
}

func (fs *osFS) SourceRoot() string { return fs.sourceRoot }

func (fs *osFS) Listdir(rel string) map[string]bool {
	rel = cleanRel(rel)
	if cached, ok := fs.dirs[rel]; ok {
		fs.listdirHits++
		return cached
	}
	fs.listdirMisses++

	full := fs.rootSlash + rel
	if rel == "" {
		full = fs.sourceRoot
	}

	entries, err := os.ReadDir(full)
	if err != nil {
		fs.dirs[rel] = nil
		return nil
	}

	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = e.IsDir()
	}
	fs.dirs[rel] = out

	return out
}

func (fs *osFS) Exists(rel string) (present bool, isDir bool) {
	rel = cleanRel(rel)
	if rel == "" {
		return true, true
	}

	dir, name := splitDirName(rel)
	entries := fs.Listdir(dir)
	if entries == nil {
		fs.existsMisses++
		return false, false
	}

	isDir, ok := entries[name]
	if ok {
		fs.existsHits++
	} else {
		fs.existsMisses++
	}

	return ok, isDir
}

func (fs *osFS) IsFile(rel string) bool {
	p, d := fs.Exists(rel)
	return p && !d
}

func (fs *osFS) IsDir(rel string) bool {
	p, d := fs.Exists(rel)
	return p && d
}

func (fs *osFS) Read(rel string) []byte {
	return Throw2(os.ReadFile(fs.rootSlash + cleanRel(rel)))
}

func (fs *osFS) ReadInto(rel string, buf []byte) []byte {
	f := Throw2(os.Open(fs.rootSlash + cleanRel(rel)))
	defer f.Close()

	buf = buf[:0]

	if fi, statErr := f.Stat(); statErr == nil {
		sz := int(fi.Size())
		if sz > cap(buf) {
			buf = make([]byte, 0, sz)
		}

		for len(buf) < sz {
			n, err := f.Read(buf[len(buf):sz])
			buf = buf[:len(buf)+n]
			if err != nil {
				if err == io.EOF {
					return buf
				}
				Throw(err)
			}
		}

		return buf
	}

	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}

		n, err := f.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err != nil {
			if err == io.EOF {
				return buf
			}
			Throw(err)
		}
	}
}

func (fs *osFS) ReadAbs(absPath string) []byte {
	return fs.Read(fs.relForAbs(absPath))
}

func (fs *osFS) ExistsAbs(absPath string) (present bool, isDir bool) {
	return fs.Exists(fs.relForAbs(absPath))
}

func (fs *osFS) relForAbs(absPath string) string {
	if absPath == fs.sourceRoot {
		return ""
	}
	if strings.HasPrefix(absPath, fs.rootSlash) {
		return absPath[len(fs.rootSlash):]
	}

	ThrowFmt("relForAbs: %q is outside source root %q", absPath, fs.sourceRoot)

	return ""
}

func (fs *osFS) Walk(rel string, visit func(rel string, isDir bool)) {
	rel = cleanRel(rel)

	present, isDir := fs.Exists(rel)
	if !present {
		return
	}

	visit(rel, isDir)

	if !isDir {
		return
	}

	prefix := rel
	if prefix != "" {
		prefix += "/"
	}

	for name, childIsDir := range fs.Listdir(rel) {
		child := prefix + name
		if childIsDir {
			fs.Walk(child, visit)
			continue
		}
		visit(child, false)
	}
}

type fsPerfStats struct {
	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
	dirsCached    int
}

func (fs *osFS) perfStats() fsPerfStats {
	return fsPerfStats{
		listdirHits:   fs.listdirHits,
		listdirMisses: fs.listdirMisses,
		existsHits:    fs.existsHits,
		existsMisses:  fs.existsMisses,
		dirsCached:    len(fs.dirs),
	}
}

func cleanRel(rel string) string {
	if rel == "" || rel == "." {
		return ""
	}

	if pathIsClean(rel) {
		return rel
	}
	rel = path.Clean(rel)
	if rel == "." || rel == "/" {
		return ""
	}
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimSuffix(rel, "/")
	return rel
}

func pathIsClean(p string) bool {
	if p[0] == '/' || p[len(p)-1] == '/' {
		return false
	}

	if p[0] == '.' {
		if len(p) == 1 || p[1] == '/' || (p[1] == '.' && (len(p) == 2 || p[2] == '/')) {
			return false
		}
	}

	for i := 0; i < len(p); i++ {
		if p[i] != '/' {
			continue
		}

		if p[i+1] == '/' {
			return false
		}
		if p[i+1] == '.' {
			if i+2 == len(p) || p[i+2] == '/' {
				return false
			}
			if p[i+2] == '.' && (i+3 == len(p) || p[i+3] == '/') {
				return false
			}
		}
	}

	return true
}

func splitDirName(rel string) (string, string) {
	i := strings.LastIndexByte(rel, '/')
	if i < 0 {
		return "", rel
	}
	return rel[:i], rel[i+1:]
}

func firstComponent(p string) (first string, more bool) {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], true
	}
	return p, false
}

func joinRel(prefix, suffix string) string {
	switch {
	case prefix == "":
		return suffix
	case suffix == "":
		return prefix
	default:
		return prefix + "/" + suffix
	}
}
