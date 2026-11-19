package main

import "syscall"

type Tree interface {
	root() string
	listdir(dir STR) DirView
	dirHas(v DirView, name string) (present bool, isDir bool)

	exists(prefix STR, suffix string) (present bool, isDir bool)
	existsID(prefix, rel STR) (present bool, isDir bool)
	isFile(prefix STR, suffix string) bool
	isFileClean(prefix STR, suffix string) bool
	isFileID(dir, name STR) bool
	isFilePathClean(rel string) bool
	isFileVFS(v VFS) bool
	noteFile(v VFS)
	isReadFile(rel STR) bool
	contentHash(rel STR) uint64
	isDir(prefix STR, suffix string) bool
	sourcePathExists(rel string) bool

	resolveSourceUnder(prefix, target STR) STR
	resolveSourceUnderClean(prefix, target STR, targetClean bool) STR

	read(rel string) [][]byte
	readPath(rel STR) [][]byte

	resourceBundle(rel STR) (map[string]ResourceBundleEntry, bool)
	storeResourceBundle(rel STR, bundle map[string]ResourceBundleEntry)

	walk(rel string, visit func(rel string, isDir bool) bool)
	walkDirID(root STR, visit func(rel string, dir STR, isDir bool) bool)
	isFileVFSAny(v VFS) bool
	testPathReadError(rel string) error

	confRoot() string

	setReadErrorHook(hook func(rel string, errno syscall.Errno))
	unreadableDir(dir string) syscall.Errno

	peekParsed(rel STR) (ParsedSource, bool)
	parse(vfsPath VFS, rel string, parser IncludeDirectiveParser, bucket ParsedIncludeBucket) ParsedSource
	includes(p ParsedSource) ParsedIncludeSet
	walkable(p ParsedSource) []IncludeDirective
}
