package main

type FS interface {
	listdir(dir STR) DirView
	dirHas(v DirView, name string) (present bool, isDir bool)

	exists(prefix STR, suffix string) (present bool, isDir bool)
	isFile(prefix STR, suffix string) bool
	isDir(prefix STR, suffix string) bool

	resolveSourceUnder(prefix, target STR) STR

	read(rel string) []byte

	walk(rel string, visit func(rel string, isDir bool) bool)

	contentHash(rel STR) uint64
}

func dirKey(dir string) STR {
	return internStr(cleanRel(dir))
}

type DirView struct {
	dir   STR
	names []uint32
}

func (v DirView) listable() bool {
	return v.names != nil
}
