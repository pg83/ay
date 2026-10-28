package main

const (
	parsedIncludesLocal ParsedIncludeBucket = iota
	parsedIncludesRagelNative

	parsedIncludesHeader
	parsedIncludesCpp

	parsedIncludesProtoConfig
	parsedIncludesAll
	parsedIncludeBucketCount
)

type ParsedIncludeBucket uint8

type ParsedInclude = IncludeDirective

type ParsedIncludeSet [parsedIncludeBucketCount][]IncludeDirective

func appendParsedDirectives(set ParsedIncludeSet, bucket ParsedIncludeBucket, directives ...IncludeDirective) ParsedIncludeSet {
	set[bucket] = append(set[bucket], directives...)

	return set
}

func (set ParsedIncludeSet) bucket(bucket ParsedIncludeBucket) []IncludeDirective {
	return set[bucket]
}

func (pm *IncludeParserManager) outputIncludes(output VFS, set ParsedIncludeSet) ParsedIncludeSet {
	if len(set[parsedIncludesLocal]) > 0 {
		if parser, _ := pm.registry.registeredParserFor(output.relString()); parser == cythonParser {
			return pm.cythonOutputIncludes(set)
		}
	}

	return set
}

type ParsedSource struct {
	local  []IncludeDirective
	extra  uint32
	parser IncludeParserKind
	bucket ParsedIncludeBucket
}

func (p ParsedSource) includes(cache *SharedParseCache) ParsedIncludeSet {
	if p.extra != 0 {
		return cache.parsedExtras.s[p.extra-1]
	}

	return ParsedIncludeSet{parsedIncludesLocal: p.local}
}

func (p ParsedSource) walkable(cache *SharedParseCache) []IncludeDirective {
	if p.bucket == parsedIncludesLocal {
		return p.local
	}

	if p.extra != 0 {
		return cache.parsedExtras.s[p.extra-1][p.bucket]
	}

	return nil
}

type SharedParseCache struct {
	notsJSON     DenseMap[STR, ParsedJSON]
	notsYAML     DenseMap[STR, map[string]any]
	notsLocks    DenseMap[STR, *NotsLockfile]
	directives   *BumpAllocator[IncludeDirective]
	parsed       DenseMap[STR, ParsedSource]
	parsedExtras Vec[ParsedIncludeSet]
	conf         *ConfState
	confFiles    DenseMap[STR, *ConfFile]
	sysinclYAMLs DenseMap[STR, []SysIncl]
	peerPolicies DenseMap[STR, *PeerPolicies]
}

func newSharedParseCache() *SharedParseCache {
	return &SharedParseCache{
		conf:       newConfState(),
		directives: newBumpAllocator[IncludeDirective](),
	}
}

type IncludeParserManager struct {
	fs              FS
	cache           *SharedParseCache
	scanConfigs     map[uint64]*ScanConfig
	scanConfigCount uint32
	addinclIndex    DenseMap[STR, []VFS]
	addinclIndexed  BitSet
	addinclArena    *BumpAllocator[VFS]
	registry        IncludeDirectiveParserRegistry
}

func newIncludeParserManagerFS(fs FS, cache *SharedParseCache) *IncludeParserManager {
	return &IncludeParserManager{
		fs:           fs,
		cache:        cache,
		addinclArena: newBumpAllocator[VFS](),
		registry:     newIncludeDirectiveParserRegistry(),
	}
}

func (pm *IncludeParserManager) protoParser() ProtoIncludeDirectiveParser {
	return pm.registry.proto
}

func (pm *IncludeParserManager) protoScanOwnPaths(outRoot VFS, includes []VFS) []VFS {
	n := 1 + len(includes)

	if outRoot != 0 {
		n++
	}

	paths := pm.addinclArena.alloc(n)
	k := 0

	paths[k] = pbRuntimeBaseVFS
	k++

	if outRoot != 0 {
		paths[k] = outRoot
		k++
	}

	copy(paths[k:], includes)
	pm.addinclArena.commit(n)

	return paths[:n:n]
}

func (pm *IncludeParserManager) sourceParsedBuckets(vfsPath VFS, ctxParser IncludeDirectiveParser) ParsedIncludeSet {
	if ctxParser == nil {
		ctxParser = pm.registry.defaultParser
	}

	return pm.sourceParsed(vfsPath, ctxParser).includes(pm.cache)
}

func (pm *IncludeParserManager) sourceParsedWalkable(vfsPath VFS, ctxParser IncludeDirectiveParser) []IncludeDirective {
	return pm.sourceParsed(vfsPath, ctxParser).walkable(pm.cache)
}

func (pm *IncludeParserManager) sourceParsed(vfsPath VFS, ctxParser IncludeDirectiveParser) ParsedSource {
	key := vfsPath.rel()

	if cached, ok := pm.cache.parsed.get(key); ok {
		return cached
	}

	rel := key.string()
	parser, registered := pm.registry.registeredParserFor(rel)
	bucket := parsedIncludesLocal

	if !registered {
		parser = ctxParser
	}

	if _, ok := parser.(RagelIncludeDirectiveParser); ok {
		bucket = parsedIncludesRagelNative
	}

	if parser == nil || !pm.fs.isFileVFS(vfsPath) {
		empty := ParsedSource{parser: includeParserKind(parser), bucket: bucket}

		pm.cache.parsed.put(key, empty)

		return empty
	}

	data := pm.fs.readPath(key)

	if first := data[0]; len(first) >= 3 && first[0] == 0xEF && first[1] == 0xBB && first[2] == 0xBF {
		data[0] = first[3:]
	}

	out := parser.parse(rel, data, pm.cache.directives)
	parsed := ParsedSource{local: out[parsedIncludesLocal], parser: includeParserKind(parser), bucket: bucket}

	for _, entries := range out[1:] {
		if entries != nil {
			pm.cache.parsedExtras.pushBack(out)
			parsed.extra = uint32(pm.cache.parsedExtras.len())

			break
		}
	}

	pm.cache.parsed.put(key, parsed)

	return parsed
}

func (pm *IncludeParserManager) indexAddincl(a VFS) {
	if a.isBuild() || a.relString() == "" {
		return
	}

	if pm.addinclIndexed.has(uint32(a)) {
		return
	}

	pm.addinclIndexed.add(uint32(a))

	base := a.relString()

	pm.fs.walk(base, func(rel string, isDir bool) bool {
		if isDir {
			return true
		}

		t := internStr(rel[len(base)+1:])
		cur, _ := pm.addinclIndex.get(t)

		pm.addinclIndex.put(t, arenaAppend(pm.addinclArena, cur, a))

		return false
	})
}

func (pm *IncludeParserManager) resolveScanConfig(ownAddIncl, peerAddIncl, baseSearchPaths []VFS) *ScanConfig {
	h := hashScanConfig(ownAddIncl, peerAddIncl, baseSearchPaths)

	if sc, ok := pm.scanConfigs[h]; ok {
		return sc
	}

	if pm.scanConfigs == nil {
		pm.scanConfigs = make(map[uint64]*ScanConfig, 256)
	}

	sc := &ScanConfig{
		num:             pm.scanConfigCount,
		ownAddIncl:      ownAddIncl,
		peerAddInclSet:  peerAddIncl,
		baseSearchPaths: baseSearchPaths,
	}

	sc.ri = buildCfgResolveIndex(sc)

	pm.scanConfigCount++
	pm.scanConfigs[h] = sc

	if sc.ri.indexable {
		for _, p := range sc.ownAddIncl {
			pm.indexAddincl(p)
		}

		for _, p := range sc.peerAddInclSet {
			pm.indexAddincl(p)
		}
	}

	return sc
}
