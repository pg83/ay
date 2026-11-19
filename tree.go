package main

const parsedExtraInImage = 1 << 31

type Tree struct {
	fs         FS
	files      *MmapedFiles
	dirty      BitSet
	parsed     DenseMap[STR, ParsedSource]
	extras     Vec[ParsedIncludeSet]
	directives *BumpAllocator[IncludeDirective]

	parses int
}

func newTree(fs FS, image *TreeImage) *Tree {
	t := &Tree{fs: fs, directives: newBumpAllocator[IncludeDirective]()}

	if image != nil {
		t.files = image.files()
	}

	return t
}

func (t *Tree) imageFile(rel STR) uint32 {
	if t.files == nil {
		return 0
	}

	no := t.files.no(rel)

	if no == 0 || t.dirty.has(no) {
		return 0
	}

	return no
}

func (t *Tree) contentHash(rel STR) uint64 {
	if h := t.fs.contentHash(rel); h != 0 {
		return h
	}

	if no := t.imageFile(rel); no != 0 {
		return t.files.hashes[no]
	}

	return 0
}

func (t *Tree) imageParsed(rel STR) (ParsedSource, bool) {
	if no := t.imageFile(rel); no != 0 {
		return t.files.parsedSource(no)
	}

	return ParsedSource{}, false
}

func (t *Tree) peekParsed(rel STR) (ParsedSource, bool) {
	if cached, ok := t.parsed.get(rel); ok {
		return cached, true
	}

	return t.imageParsed(rel)
}

func (t *Tree) parse(vfsPath VFS, rel string, parser IncludeDirectiveParser, bucket ParsedIncludeBucket) ParsedSource {
	key := vfsPath.rel()

	if parser == nil || !t.fs.isFileVFS(vfsPath) {
		empty := ParsedSource{parser: includeParserKind(parser), bucket: bucket}

		t.parsed.put(key, empty)

		return empty
	}

	t.parses++

	data := t.fs.readPath(key)

	if first := data[0]; len(first) >= 3 && first[0] == 0xEF && first[1] == 0xBB && first[2] == 0xBF {
		data[0] = first[3:]
	}

	out := parser.parse(rel, data, t.directives)
	parsed := ParsedSource{local: out[parsedIncludesLocal], parser: includeParserKind(parser), bucket: bucket}

	if len(parsed.local) > 0 {
		parsed.dir = internDirText(pathDir(rel))
	}

	for _, entries := range out[1:] {
		if entries != nil {
			t.extras.pushBack(out)
			parsed.extra = uint32(t.extras.len())

			if parsed.dir == 0 {
				parsed.dir = internDirText(pathDir(rel))
			}

			break
		}
	}

	t.parsed.put(key, parsed)

	return parsed
}

func (t *Tree) includes(p ParsedSource) ParsedIncludeSet {
	switch {
	case p.extra == 0:
		return ParsedIncludeSet{parsedIncludesLocal: p.local}
	case p.extra&parsedExtraInImage != 0:
		return t.files.extraSet(p.extra &^ parsedExtraInImage)
	}

	return t.extras.s[p.extra-1]
}

func (t *Tree) walkable(p ParsedSource) []IncludeDirective {
	switch {
	case p.bucket == parsedIncludesLocal:
		return p.local
	case p.extra == 0:
		return nil
	case p.extra&parsedExtraInImage != 0:
		return t.files.span(t.files.extras[p.extra&^parsedExtraInImage-1][p.bucket])
	}

	return t.extras.s[p.extra-1][p.bucket]
}

func (t *Tree) writeFiles(w *TreeImageWriter) {
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
		hash := t.contentHash(id)
		p, ok := t.peekParsed(id)

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

				for bucket, list := range t.includes(p) {
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
