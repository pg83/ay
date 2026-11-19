package main

import "syscall"

const parsedExtraInImage = 1 << 31

type ImageTree struct {
	inner Tree
	files *MmapedFiles
	dirty BitSet
}

func newImageTree(image *TreeImage, inner Tree) *ImageTree {
	return &ImageTree{inner: inner, files: image.files()}
}

func (t *ImageTree) imageFile(rel STR) uint32 {
	no := t.files.no(rel)

	if no == 0 || t.dirty.has(no) {
		return 0
	}

	return no
}

func (t *ImageTree) contentHash(rel STR) uint64 {
	if h := t.inner.contentHash(rel); h != 0 {
		return h
	}

	if no := t.imageFile(rel); no != 0 {
		return t.files.hashes[no]
	}

	return 0
}

func (t *ImageTree) isReadFile(rel STR) bool {
	return t.contentHash(rel) != 0
}

func (t *ImageTree) peekParsed(rel STR) (ParsedSource, bool) {
	if cached, ok := t.inner.peekParsed(rel); ok {
		return cached, true
	}

	if no := t.imageFile(rel); no != 0 {
		return t.files.parsedSource(no)
	}

	return ParsedSource{}, false
}

func (t *ImageTree) parse(vfsPath VFS, rel string, parser IncludeDirectiveParser, bucket ParsedIncludeBucket) ParsedSource {
	return t.inner.parse(vfsPath, rel, parser, bucket)
}

func (t *ImageTree) includes(p ParsedSource) ParsedIncludeSet {
	if p.extra&parsedExtraInImage != 0 {
		return t.files.extraSet(p.extra &^ parsedExtraInImage)
	}

	return t.inner.includes(p)
}

func (t *ImageTree) walkable(p ParsedSource) []IncludeDirective {
	if p.bucket != parsedIncludesLocal && p.extra&parsedExtraInImage != 0 {
		return t.files.span(t.files.extras[p.extra&^parsedExtraInImage-1][p.bucket])
	}

	return t.inner.walkable(p)
}

func (t *ImageTree) root() string {
	return t.inner.root()
}

func (t *ImageTree) listdir(dir STR) DirView {
	return t.inner.listdir(dir)
}

func (t *ImageTree) dirHas(v DirView, name string) (present bool, isDir bool) {
	return t.inner.dirHas(v, name)
}

func (t *ImageTree) exists(prefix STR, suffix string) (present bool, isDir bool) {
	return t.inner.exists(prefix, suffix)
}

func (t *ImageTree) existsID(prefix, rel STR) (present bool, isDir bool) {
	return t.inner.existsID(prefix, rel)
}

func (t *ImageTree) isFile(prefix STR, suffix string) bool {
	return t.inner.isFile(prefix, suffix)
}

func (t *ImageTree) isFileClean(prefix STR, suffix string) bool {
	return t.inner.isFileClean(prefix, suffix)
}

func (t *ImageTree) isFileID(dir, name STR) bool {
	return t.inner.isFileID(dir, name)
}

func (t *ImageTree) isFilePathClean(rel string) bool {
	return t.inner.isFilePathClean(rel)
}

func (t *ImageTree) isFileVFS(v VFS) bool {
	return t.inner.isFileVFS(v)
}

func (t *ImageTree) noteFile(v VFS) {
	t.inner.noteFile(v)
}

func (t *ImageTree) isDir(prefix STR, suffix string) bool {
	return t.inner.isDir(prefix, suffix)
}

func (t *ImageTree) sourcePathExists(rel string) bool {
	return t.inner.sourcePathExists(rel)
}

func (t *ImageTree) resolveSourceUnder(prefix, target STR) STR {
	return t.inner.resolveSourceUnder(prefix, target)
}

func (t *ImageTree) resolveSourceUnderClean(prefix, target STR, targetClean bool) STR {
	return t.inner.resolveSourceUnderClean(prefix, target, targetClean)
}

func (t *ImageTree) read(rel string) [][]byte {
	return t.inner.read(rel)
}

func (t *ImageTree) readPath(rel STR) [][]byte {
	return t.inner.readPath(rel)
}

func (t *ImageTree) resourceBundle(rel STR) (map[string]ResourceBundleEntry, bool) {
	return t.inner.resourceBundle(rel)
}

func (t *ImageTree) storeResourceBundle(rel STR, bundle map[string]ResourceBundleEntry) {
	t.inner.storeResourceBundle(rel, bundle)
}

func (t *ImageTree) walk(rel string, visit func(rel string, isDir bool) bool) {
	t.inner.walk(rel, visit)
}

func (t *ImageTree) walkDirID(root STR, visit func(rel string, dir STR, isDir bool) bool) {
	t.inner.walkDirID(root, visit)
}

func (t *ImageTree) isFileVFSAny(v VFS) bool {
	return t.inner.isFileVFSAny(v)
}

func (t *ImageTree) testPathReadError(rel string) error {
	return t.inner.testPathReadError(rel)
}

func (t *ImageTree) confRoot() string {
	return t.inner.confRoot()
}

func (t *ImageTree) setReadErrorHook(hook func(rel string, errno syscall.Errno)) {
	t.inner.setReadErrorHook(hook)
}

func (t *ImageTree) unreadableDir(dir string) syscall.Errno {
	return t.inner.unreadableDir(dir)
}

func writeTreeFiles(w *TreeImageWriter, tree Tree) {
	count := internTable.count
	fileNo := make([]uint32, count)
	hashes := []uint64{0}
	parsed := []MmapedParsed{{}}

	var directives []IncludeDirective
	var extras []MmapedExtra

	add := func(list []IncludeDirective) MmapedSpan {
		if list == nil {
			return MmapedSpan{off: mmapedNilSpan}
		}

		s := MmapedSpan{off: uint32(len(directives)), n: uint32(len(list))}

		directives = append(directives, list...)

		return s
	}

	for id := STR(1); uint32(id) < count; id++ {
		hash := tree.contentHash(id)
		p, ok := tree.peekParsed(id)

		if hash == 0 && !ok {
			continue
		}

		fileNo[id] = uint32(len(hashes))
		hashes = append(hashes, hash)

		var rec MmapedParsed

		if ok {
			rec = MmapedParsed{local: add(p.local), dir: p.dir, parser: p.parser, bucket: p.bucket, flags: mmapedParsedPresent}

			if p.extra != 0 {
				var e MmapedExtra

				for bucket, list := range tree.includes(p) {
					if bucket == int(parsedIncludesLocal) {
						e[bucket] = rec.local
					} else {
						e[bucket] = add(list)
					}
				}

				extras = append(extras, e)
				rec.extra = uint32(len(extras))
			}
		}

		parsed = append(parsed, rec)
	}

	w.section(treeSectionFileNo, sliceBytesOf(fileNo))
	w.section(treeSectionFileHash, sliceBytesOf(hashes))
	w.section(treeSectionFileParsed, sliceBytesOf(parsed))
	w.section(treeSectionDirectives, sliceBytesOf(directives))
	w.section(treeSectionParsedExtras, sliceBytesOf(extras))
}
