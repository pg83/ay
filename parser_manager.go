package main

import (
	"path"
	"strings"
)

// parsedIncludeBucket enumerates the fixed, compile-time-known groups a parser
// sorts a file's #include directives into. Used as the index of parsedIncludeSet.
type ParsedIncludeBucket uint8

const (
	parsedIncludesLocal ParsedIncludeBucket = iota
	parsedIncludesRagelNative
	// parsedIncludesHeader is consumed by header (.h) outputs; parsedIncludesCpp by
	// translation-unit (.cc/.cpp) outputs. INDUCED_DEPS(h …) lands in Header only,
	// (cpp …) in Cpp only, and (h+cpp …) is split into BOTH. Parse-derived directives
	// that apply to either consumer (proto/ev .pb.h, ragel self-include, swig induced)
	// are likewise written to both buckets.
	parsedIncludesHeader
	parsedIncludesCpp
	parsedIncludeBucketCount
)

type ParsedInclude = IncludeDirective

// parsedIncludeSet groups a source file's parsed directives by bucket. The bucket
// set is fixed at compile time, so it is a flat array indexed by the bucket enum —
// a direct index, not a map probe. The zero value is the empty set.
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

// sharedParseCache memoizes the parsed-#include result of a source file by VFS
// path alone. The parse step is context-free by construction — the directives
// extracted from a file depend on the file content only, not on which module
// requested the parse — so this cache is correct to share globally across all
// scan contexts. It mirrors upstream's TParsersCache (see
// yatool/devtools/ymake/include_processors/parsers_cache.h, key is
// (parserId, fileId) with no module component). The single-visit invariant
// enforced upstream by TUpdEntryStats::OnceProcessedAsFile (add_iter.h:377)
// means each file is parsed exactly once per run; we get the same behaviour
// from this map's first-write-wins semantics. Do not key by scan context.
type SharedParseCache struct {
	// ambiguous holds parse results for files with UNREGISTERED extensions,
	// keyed by splitMix64(strID, parserID): such a file parses under the scan
	// context's parser (resolved once from the walk's root), so one file may
	// legitimately carry one result per parser (.i under swig vs under C).
	ambiguous map[uint64]ParsedIncludeSet

	// parsed is keyed by the source file's STR (vfsPath.strID()): a parsed source
	// is always Source-rooted (VFS == STR<<1), so the STR is lossless and halves
	// DenseMap's idx array versus the 2x-wider VFS space. A present slot with an
	// empty set is the negative cache (file does not exist or has no directives),
	// distinct from absent (not yet parsed); the DenseMap present/absent bool, not
	// the value, gates the re-stat.
	parsed DenseMap[STR, ParsedIncludeSet]

	parsedHits   uint64
	parsedMisses uint64
}

func newSharedParseCache() *SharedParseCache {
	return &SharedParseCache{ambiguous: make(map[uint64]ParsedIncludeSet, 16)}
}

type IncludeParserManager struct {
	fs    FS
	cache *SharedParseCache

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

// sourceParsedBuckets parses vfsPath under its extension's registered parser,
// or — for unregistered extensions — under ctxParser, the scan context's
// parser resolved once from the walk's root (nil falls back to the C-like
// default). Registered-ext results cache by file; ambiguous ones by
// (file, parser).
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

	out := parser.parse(rel, data)
	out = pm.withCythonSibling(rel, out)

	return put(out)
}

// withCythonSibling models Cython's implicit sibling .pxd: a .pyx uses its
// same-named .pxd only when that file exists. Emitting it unconditionally (as a
// pure parser must, lacking FS access) yields a spurious unresolved include
// whenever the sibling is absent; gate it here, where the FS is available. .pyx
// files are rare, so the extra existence probe is off the hot C/C++ parse path.
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

func (pm *IncludeParserManager) registerBuildParsedIncludes(out VFS, parsed []IncludeDirective) {
	// buildParsed is keyed by VFS and consulted only for build-rooted paths
	// (parsedIncludes gates on IsBuild), so a source-rooted registration would
	// be silently unreachable.
	if !out.isBuild() {
		ThrowFmt("RegisterBuildParsedIncludes: source-rooted output %q", out.String())
	}

	pm.buildParsed[out] = parsed
}

func (pm *IncludeParserManager) indexAddincl(a VFS) {
	if a.root() != VFSRootSource || a.rel() == "" {
		return
	}

	if pm.addinclIndexed.has(uint32(a)) {
		return
	}

	pm.addinclIndexed.add(uint32(a))
	base := a.rel()
	pm.fs.walk(base, func(rel string, isDir bool) {
		if isDir {
			return
		}

		t := internStr(rel[len(base)+1:])
		cur, _ := pm.addinclIndex.get(t)
		pm.addinclIndex.put(t, append(cur, a))
	})
}

func (pm *IncludeParserManager) perfStats() ParserPerfStats {
	return ParserPerfStats{
		parsedHits:   pm.cache.parsedHits,
		parsedMisses: pm.cache.parsedMisses,
		buildParsed:  len(pm.buildParsed),
	}
}
