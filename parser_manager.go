package main

import (
	"path"
	"strings"
)

type ParsedIncludeBucket uint8

const (
	parsedIncludesLocal ParsedIncludeBucket = iota
	parsedIncludesRagelNative

	parsedIncludesHeader
	parsedIncludesCpp

	parsedIncludesProtoConfig
	parsedIncludeBucketCount
)

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
	ambiguous    map[uint64]ParsedIncludeSet
	directives   *BumpAllocator[IncludeDirective]
	parsed       DenseMap[STR, ParsedIncludeSet]
	parsedHits   uint64
	parsedMisses uint64
}

func newSharedParseCache() *SharedParseCache {
	return &SharedParseCache{
		ambiguous:  make(map[uint64]ParsedIncludeSet, 16),
		directives: newBumpAllocator[IncludeDirective](directiveBlockHint),
	}
}

type IncludeParserManager struct {
	fs              FS
	cache           *SharedParseCache
	scanConfigs     map[uint64]*ScanConfig
	scanConfigCount uint32
	addinclIndex    DenseMap[STR, []VFS]
	addinclIndexed  BitSet
	buildParsed     map[VFS][]IncludeDirective
}

type ParserPerfStats struct {
	parsedHits   uint64
	parsedMisses uint64
	buildParsed  int
}

func newIncludeParserManagerFS(fs FS, cache *SharedParseCache) *IncludeParserManager {
	return &IncludeParserManager{
		fs:          fs,
		cache:       cache,
		buildParsed: make(map[VFS][]IncludeDirective, 256),
	}
}

func (pm *IncludeParserManager) sourceParsedBuckets(vfsPath VFS, ctxParser IncludeDirectiveParser) ParsedIncludeSet {
	key := STR(vfsPath.strID())
	rel := vfsPath.rel()
	parser := includeDirectiveParsers.registeredParserFor(rel)
	var ambKey uint64

	if parser == nil {
		parser = ctxParser

		if parser == nil {
			parser = includeDirectiveParsers.defaultParser
		}

		ambKey = splitMix64(uint32(key), parser.id())

		if cached, ok := pm.cache.ambiguous[ambKey]; ok {
			pm.cache.parsedHits++

			return cached
		}
	} else if cached, ok := pm.cache.parsed.get(key); ok {
		pm.cache.parsedHits++

		return cached
	}

	pm.cache.parsedMisses++

	put := func(set ParsedIncludeSet) ParsedIncludeSet {
		if ambKey != 0 {
			pm.cache.ambiguous[ambKey] = set
		} else {
			pm.cache.parsed.put(key, set)
		}

		return set
	}

	if !pm.fs.isFile(srcRootVFS, rel) {
		return put(ParsedIncludeSet{})
	}

	data := pm.fs.read(rel)

	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	out := parser.parse(rel, data, pm.cache.directives)
	out = pm.withCythonSibling(rel, out)

	return put(out)
}

func (pm *IncludeParserManager) withCythonSibling(rel string, set ParsedIncludeSet) ParsedIncludeSet {
	if !strings.HasSuffix(rel, ".pyx") {
		return set
	}

	sibling := rel[:len(rel)-len(".pyx")] + ".pxd"
	sibDir, sibBase := splitDirName(sibling)

	if !pm.fs.isFile(dirKey(sibDir), sibBase) {
		return set
	}

	d := IncludeDirective{kind: includeQuoted, target: internStr(path.Base(sibling))}
	local := set.bucket(parsedIncludesLocal)
	merged := make([]IncludeDirective, 0, 1+len(local))
	merged = append(merged, d)
	merged = append(merged, local...)

	set[parsedIncludesLocal] = merged

	return set
}

func (pm *IncludeParserManager) parsedIncludes(vfsPath VFS, ctxParser IncludeDirectiveParser) []IncludeDirective {
	if vfsPath.isBuild() {
		if parsed, ok := pm.buildParsed[vfsPath]; ok {
			return parsed
		}

		return nil
	}

	return pm.sourceParsedBuckets(vfsPath, ctxParser).bucket(walkableBucketFor(vfsPath.rel()))
}

func (pm *IncludeParserManager) injectSourceParse(vfsPath VFS, set ParsedIncludeSet) {
	pm.cache.parsed.put(STR(vfsPath.strID()), set)
}

func (pm *IncludeParserManager) registerBuildParsedIncludes(out VFS, parsed []IncludeDirective) {
	if !out.isBuild() {
		throwFmt("RegisterBuildParsedIncludes: source-rooted output %q", out.string())
	}

	pm.buildParsed[out] = parsed
}

func (pm *IncludeParserManager) indexAddincl(a VFS) {
	if a.isBuild() || a.rel() == "" {
		return
	}

	if pm.addinclIndexed.has(uint32(a)) {
		return
	}

	pm.addinclIndexed.add(uint32(a))
	base := a.rel()
	pm.fs.walk(base, func(rel string, isDir bool) bool {
		if isDir {
			return true
		}

		t := internStr(rel[len(base)+1:])
		cur, _ := pm.addinclIndex.get(t)
		pm.addinclIndex.put(t, append(cur, a))

		return false
	})
}

func (pm *IncludeParserManager) resolveScanConfig(cfg *ScanContext) *ScanConfig {
	h := hashScanContext(cfg)

	if sc, ok := pm.scanConfigs[h]; ok {
		return sc
	}

	if pm.scanConfigs == nil {
		pm.scanConfigs = make(map[uint64]*ScanConfig, 256)
	}

	sc := &ScanConfig{num: pm.scanConfigCount, ri: buildCfgResolveIndex(cfg)}
	pm.scanConfigCount++
	pm.scanConfigs[h] = sc

	if sc.ri.indexable {
		for _, p := range cfg.OwnAddIncl {
			pm.indexAddincl(p)
		}

		for _, p := range cfg.PeerAddInclSet {
			pm.indexAddincl(p)
		}
	}

	return sc
}

func (pm *IncludeParserManager) perfStats() ParserPerfStats {
	return ParserPerfStats{
		parsedHits:   pm.cache.parsedHits,
		parsedMisses: pm.cache.parsedMisses,
		buildParsed:  len(pm.buildParsed),
	}
}
