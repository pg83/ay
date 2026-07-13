package main

const (
	parsedIncludesLocal ParsedIncludeBucket = iota
	parsedIncludesRagelNative

	parsedIncludesHeader
	parsedIncludesCpp

	parsedIncludesProtoConfig
	parsedIncludeBucketCount
)

type ParsedIncludeBucket uint8

type ParsedInclude = IncludeDirective

type ParsedIncludeSet [parsedIncludeBucketCount][]IncludeDirective

func appendParsedDirectives(set ParsedIncludeSet, bucket ParsedIncludeBucket, directives ...IncludeDirective) ParsedIncludeSet {
	if len(directives) == 0 {
		return set
	}

	set[bucket] = append(set[bucket], directives...)

	return set
}

func (set ParsedIncludeSet) bucket(bucket ParsedIncludeBucket) []IncludeDirective {
	return set[bucket]
}

type SharedParseCache struct {
	ambiguous  map[uint64]ParsedIncludeSet
	directives *BumpAllocator[IncludeDirective]
	parsed     DenseMap[STR, ParsedIncludeSet]
}

func newSharedParseCache() *SharedParseCache {
	return &SharedParseCache{
		ambiguous:  make(map[uint64]ParsedIncludeSet, 16),
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
	key := vfsPath.rel()
	rel := key.string()
	parser := pm.registry.registeredParserFor(rel)

	var ambKey uint64

	if parser == nil {
		parser = ctxParser

		if parser == nil {
			parser = pm.registry.defaultParser
		}

		ambKey = splitMix64(uint32(key), parser.id())

		if cached, ok := pm.cache.ambiguous[ambKey]; ok {
			return cached
		}
	} else if cached, ok := pm.cache.parsed.get(key); ok {
		return cached
	}

	if !pm.fs.isFileClean(srcRootRel, rel) {
		var empty ParsedIncludeSet

		if ambKey != 0 {
			pm.cache.ambiguous[ambKey] = empty
		} else {
			pm.cache.parsed.put(key, empty)
		}

		return empty
	}

	data := pm.fs.read(rel)

	if first := data[0]; len(first) >= 3 && first[0] == 0xEF && first[1] == 0xBB && first[2] == 0xBF {
		data[0] = first[3:]
	}

	out := parser.parse(rel, data, pm.cache.directives)

	if ambKey != 0 {
		pm.cache.ambiguous[ambKey] = out
	} else {
		pm.cache.parsed.put(key, out)
	}

	return out
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
