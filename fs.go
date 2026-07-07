package main

type FS interface {
	listdir(dir VFS) DirView
	dirHas(v DirView, name string) (present bool, isDir bool)

	exists(prefix VFS, suffix string) (present bool, isDir bool)
	isFile(prefix VFS, suffix string) bool
	isDir(prefix VFS, suffix string) bool

	read(rel string) []byte

	walk(rel string, visit func(rel string, isDir bool) bool)

	contentHash(v VFS) uint64
}

func dirKey(dir string) VFS {
	return source(cleanRel(dir))
}

type DirView struct {
	dir   VFS
	names []uint32
}

func (v DirView) listable() bool {
	return v.names != nil
}
