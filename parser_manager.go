package main

type parsedIncludeBucket string

const (
	parsedIncludesLocal parsedIncludeBucket = "local"
	parsedIncludesCPP   parsedIncludeBucket = "cpp"
	parsedIncludesHCPP  parsedIncludeBucket = "h+cpp"
)

type parsedInclude = includeDirective

type parsedIncludeSet map[parsedIncludeBucket][]includeDirective

func rawParsedIncludeSet(bucket parsedIncludeBucket, directives ...includeDirective) parsedIncludeSet {
	if len(directives) == 0 {
		return nil
	}

	out := append([]includeDirective(nil), directives...)

	return parsedIncludeSet{bucket: out}
}

func appendParsedDirectives(set parsedIncludeSet, bucket parsedIncludeBucket, directives ...includeDirective) parsedIncludeSet {
	if len(directives) == 0 {
		return set
	}
	if set == nil {
		set = make(parsedIncludeSet)
	}
	set[bucket] = append(set[bucket], directives...)

	return set
}

func (set parsedIncludeSet) bucket(bucket parsedIncludeBucket) []includeDirective {
	if set == nil {
		return nil
	}

	return set[bucket]
}

type sharedParseCache struct {
	parsed map[VFS]parsedIncludeSet

	parsedHits   uint64
	parsedMisses uint64
}

func newSharedParseCache() *sharedParseCache {
	return &sharedParseCache{
		parsed: make(map[VFS]parsedIncludeSet, 8192),
	}
}

type includeParserManager struct {
	fs    *FS
	cache *sharedParseCache

	addinclIndex   map[STR][]VFS
	addinclIndexed map[VFS]struct{}

	buildParsed map[string][]includeDirective

	readBuf []byte
}

type parserPerfStats struct {
	parsedHits   uint64
	parsedMisses uint64
	buildParsed  int
}

func newIncludeParserManager(sourceRoot string) *includeParserManager {
	return newIncludeParserManagerFS(NewFS(sourceRoot), newSharedParseCache())
}

func newIncludeParserManagerFS(fs *FS, cache *sharedParseCache) *includeParserManager {
	return &includeParserManager{
		fs:             fs,
		cache:          cache,
		buildParsed:    make(map[string][]includeDirective, 256),
		addinclIndex:   make(map[STR][]VFS, 1<<16),
		addinclIndexed: make(map[VFS]struct{}, 1024),
	}
}

func (pm *includeParserManager) sourceParsedBuckets(vfsPath VFS) parsedIncludeSet {
	if cached, ok := pm.cache.parsed[vfsPath]; ok {
		pm.cache.parsedHits++
		return cached
	}

	pm.cache.parsedMisses++

	rel := vfsPath.Rel()

	if !pm.fs.IsFile(rel) {
		pm.cache.parsed[vfsPath] = nil

		return nil
	}

	data := pm.fs.ReadInto(rel, pm.readBuf)
	pm.readBuf = data

	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	out := includeDirectiveParsers.parserFor(rel).Parse(rel, data)
	pm.cache.parsed[vfsPath] = out

	return out
}

func (pm *includeParserManager) parsedIncludes(vfsPath VFS) []includeDirective {
	if vfsPath.IsBuild() {
		if parsed, ok := pm.buildParsed[vfsPath.Rel()]; ok {
			return parsed
		}

		return nil
	}

	return pm.sourceParsedBuckets(vfsPath).bucket(parsedIncludesLocal)
}

func (pm *includeParserManager) RegisterBuildParsedIncludes(rel string, parsed []includeDirective) {
	pm.buildParsed[rel] = parsed
}

func (pm *includeParserManager) indexAddincl(a VFS) {
	if a.Root() != VFSRootSource || a.Rel() == "" {
		return
	}
	if _, done := pm.addinclIndexed[a]; done {
		return
	}
	pm.addinclIndexed[a] = struct{}{}

	base := a.Rel()
	pm.fs.Walk(base, func(rel string, isDir bool) {
		if isDir {
			return
		}
		t := internString(rel[len(base)+1:])
		pm.addinclIndex[t] = append(pm.addinclIndex[t], a)
	})
}

func (pm *includeParserManager) fileExistsByRel(rel string) bool {
	return pm.fs.IsFile(rel)
}

func (pm *includeParserManager) perfStats() parserPerfStats {
	return parserPerfStats{
		parsedHits:   pm.cache.parsedHits,
		parsedMisses: pm.cache.parsedMisses,
		buildParsed:  len(pm.buildParsed),
	}
}
