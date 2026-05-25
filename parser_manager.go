package main

// parser_manager.go — source-tree file access + raw include scanning.
//
// This is the layer below IncludeScanner's resolver/closure engine:
// it owns SOURCE_ROOT FS access, the shared parse/existence caches, and
// dispatches to the per-extension raw scanners in parsers.go.

// sharedParseCache holds the parse-level caches that are architecture-
// independent: file-byte parsing (parsed) and file existence (exists).
// Both depend only on the source tree, not on which sysincl YAML records
// are loaded, so target/host scanner pairs in GenWith share one cache.
//
// Full unification is not safe: sysincl resolution IS arch-dependent
// (linux-musl-aarch64.yml vs linux-musl.yml map bits/* headers to
// different paths). The resolve chain (resolveCache, subgraphCache,
// sysincl{Source,Includer}Cache) stays per-scanner.
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
	// parsed memoises raw parser results per VFS-rooted path
	// ($(S)/<rel>). 8192 pre-size covers the tools/archiver peak
	// (4354 target + 3559 host, mostly overlapping).
	parsed map[VFS]parsedIncludeSet

	// Perf counters are plain uint64: generation runs single-threaded.
	parsedHits   uint64
	parsedMisses uint64
}

// newSharedParseCache allocates a sharedParseCache with the pre-sized
// parsed-include map. File existence and directory listings are owned
// by FS (shared across the entire Gen run).
func newSharedParseCache() *sharedParseCache {
	return &sharedParseCache{
		parsed: make(map[VFS]parsedIncludeSet, 8192),
	}
}

type includeParserManager struct {
	fs    *FS
	cache *sharedParseCache
	// buildParsed is the parser-layer VFS overlay for generated
	// outputs. Emitters register `$(B)` paths here explicitly; parser
	// lookup for build-rooted paths consults ONLY this map.
	buildParsed map[string][]includeDirective
}

type parserPerfStats struct {
	parsedHits   uint64
	parsedMisses uint64
	buildParsed  int
}

// newIncludeParserManager constructs a parser manager with a fresh FS
// rooted at sourceRoot. Test-only entry; production wires an externally
// constructed FS through newIncludeParserManagerFS so the same FS is
// shared with the rest of the Gen run.
func newIncludeParserManager(sourceRoot string) *includeParserManager {
	return newIncludeParserManagerFS(NewFS(sourceRoot), newSharedParseCache())
}

func newIncludeParserManagerFS(fs *FS, cache *sharedParseCache) *includeParserManager {
	return &includeParserManager{
		fs:          fs,
		cache:       cache,
		buildParsed: make(map[string][]includeDirective, 256),
	}
}

// sourceParsedBuckets returns the full parser result for a SOURCE_ROOT-
// relative file, dispatching to a per-extension parser from parsers.go.
// Memoised by VFS path; returns nil for missing files (DFS may reach
// dangling sysincl mappings).
func (pm *includeParserManager) sourceParsedBuckets(rel string) parsedIncludeSet {
	vfsPath := Source(rel)
	if cached, ok := pm.cache.parsed[vfsPath]; ok {
		pm.cache.parsedHits++
		return cached
	}

	pm.cache.parsedMisses++

	data, err := pm.fs.Read(rel)
	if err != nil {
		pm.cache.parsed[vfsPath] = nil

		return nil
	}

	// Strip a leading UTF-8 BOM (EF BB BF) before parsing: some sources
	// (e.g. library/cpp/threading/future/subscription/subscription.cpp)
	// carry one, and it would otherwise hide the first `#include` from the
	// line-oriented parsers, collapsing the whole include closure. ymake
	// ignores the BOM the same way.
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	out := includeDirectiveParsers.parserFor(rel).Parse(rel, data)
	pm.cache.parsed[vfsPath] = out

	return out
}

// parsedIncludes is the flat parser-VFS view consumed by the scanner:
// SOURCE-rooted paths expose the source parser's `local` bucket;
// BUILD-rooted paths expose only what emitters registered in buildParsed.
func (pm *includeParserManager) parsedIncludes(vfsPath VFS) []includeDirective {
	if vfsPath.IsBuild() {
		if parsed, ok := pm.buildParsed[vfsPath.Rel()]; ok {
			return parsed
		}

		return nil
	}

	return pm.sourceParsedBuckets(vfsPath.Rel()).bucket(parsedIncludesLocal)
}

func (pm *includeParserManager) RegisterBuildParsedIncludes(rel string, parsed []includeDirective) {
	pm.buildParsed[rel] = parsed
}

// fileExistsByRel is the inner, rel-keyed existence check.
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
