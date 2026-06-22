package main

import (
	"path"
	"strings"
)

// parsedIncludeBucket enumerates the fixed groups a parser sorts a file's #include
// directives into; the index of parsedIncludeSet.
type ParsedIncludeBucket uint8

const (
	parsedIncludesLocal ParsedIncludeBucket = iota
	parsedIncludesRagelNative
	// parsedIncludesHeader is consumed by header (.h) outputs, parsedIncludesCpp by
	// translation-unit outputs. INDUCED_DEPS(h/cpp/h+cpp …) routes accordingly; directives
	// applying to either consumer go to both.
	parsedIncludesHeader
	parsedIncludesCpp
	// parsedIncludesProtoConfig holds a .cfgproto source's file-level angle-include
	// headers, seeding the generated .pb.h's includes. Kept off the proto import buckets
	// so it is not walked as an import nor namespace-rewritten.
	parsedIncludesProtoConfig
	parsedIncludeBucketCount
)

type ParsedInclude = IncludeDirective

// parsedIncludeSet groups a source file's parsed directives by bucket: a flat array
// indexed by the bucket enum, zero value being the empty set.
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

// sharedParseCache memoizes a source file's parsed-#include result by VFS path alone.
// The parse step is context-free, so the cache shares globally across scan contexts;
// first-write-wins matches upstream's parse-once-per-run. Do not key by scan context.
type SharedParseCache struct {
	// ambiguous holds results for UNREGISTERED extensions, keyed by
	// splitMix64(strID, parserID): such a file parses under the scan context's parser,
	// so it may carry one result per parser.
	ambiguous map[uint64]ParsedIncludeSet

	// directives is the arena behind every retained parse result; the cached sets hold
	// its address-stable sub-slices.
	directives *BumpAllocator[IncludeDirective]

	// parsed is keyed by the source file's STR (a parsed source is always Source-rooted,
	// so the STR is lossless). A present-but-empty slot is the negative cache.
	parsed DenseMap[STR, ParsedIncludeSet]

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
	fs    FS
	cache *SharedParseCache

	// scanConfigs dedupes resolved scan configs by content hash, shared by both scanners.
	scanConfigs     map[uint64]*ScanConfig
	scanConfigCount uint32

	addinclIndex   DenseMap[STR, []VFS]
	addinclIndexed BitSet
	buildParsed    map[VFS][]IncludeDirective
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

// sourceParsedBuckets parses vfsPath under its extension's registered parser, or —
// for unregistered extensions — under ctxParser (nil falls back to the C-like default).
// Registered-ext results cache by file; ambiguous ones by (file, parser).
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

// withCythonSibling models Cython's implicit sibling .pxd: a .pyx uses its same-named
// .pxd only when it exists. A pure parser would emit it unconditionally, yielding a
// spurious unresolved include when absent; gate it here, where the FS is available.
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

// injectSourceParse records a precomputed parse under a build-generated file's
// SOURCE-rooted VFS, so the source-path readers resolve a generated proto whose on-disk
// source does not exist.
func (pm *IncludeParserManager) injectSourceParse(vfsPath VFS, set ParsedIncludeSet) {
	pm.cache.parsed.put(STR(vfsPath.strID()), set)
}

func (pm *IncludeParserManager) registerBuildParsedIncludes(out VFS, parsed []IncludeDirective) {
	// buildParsed is consulted only for build-rooted paths, so a source-rooted
	// registration would be silently unreachable.
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

// resolveScanConfig returns the deduped ScanConfig for cfg's resolve-relevant content,
// building the resolve index (and feeding the addincl inverted index) on first sight.
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
