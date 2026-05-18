package main

import "os"

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
	parsed VFSMap[parsedIncludeSet]
	// exists memoises os.Stat results, keyed by SOURCE_ROOT-relative
	// tail. 16384 covers the observed peak.
	exists map[string]bool

	// Perf counters are plain uint64: generation runs single-threaded.
	parsedHits   uint64
	parsedMisses uint64
	existsHits   uint64
	existsMisses uint64
}

// newSharedParseCache allocates a sharedParseCache with pre-sized maps
// matched to the observed peak for the tools/archiver closure.
func newSharedParseCache() *sharedParseCache {
	return &sharedParseCache{
		parsed: NewVFSMap[parsedIncludeSet](8192),
		exists: make(map[string]bool, 16384),
	}
}

type includeParserManager struct {
	sourceRoot      string
	sourceRootSlash string
	cache           *sharedParseCache
	// buildParsed is the parser-layer VFS overlay for generated
	// outputs. Emitters register `$(B)` paths here explicitly; parser
	// lookup for build-rooted paths consults ONLY this map.
	buildParsed map[string][]includeDirective
}

type parserPerfStats struct {
	parsedHits   uint64
	parsedMisses uint64
	existsHits   uint64
	existsMisses uint64
	buildParsed  int
}

func newIncludeParserManager(sourceRoot string) *includeParserManager {
	return newIncludeParserManagerWithCache(sourceRoot, newSharedParseCache())
}

func newIncludeParserManagerWithCache(sourceRoot string, cache *sharedParseCache) *includeParserManager {
	return &includeParserManager{
		sourceRoot:      sourceRoot,
		sourceRootSlash: sourceRoot + "/",
		cache:           cache,
		buildParsed:     make(map[string][]includeDirective, 256),
	}
}

// scanDirectives returns the raw include directives for the $(S)/-
// rooted file `vfsPath`. The FS translation happens here at the
// os.ReadFile call; raw parsing itself is delegated to a per-extension
// parser from parsers.go. Memoised by VFS path; returns nil for missing
// files (DFS may reach dangling sysincl mappings).
//
// sourceParsedBuckets returns the full parser result for one source
// file. Parameter is SOURCE_ROOT-relative.
func (pm *includeParserManager) sourceParsedBuckets(rel string) parsedIncludeSet {
	vfsPath := Source(rel)
	if cached, ok := pm.cache.parsed.Get(vfsPath); ok {
		pm.cache.parsedHits++
		return cached
	}

	pm.cache.parsedMisses++

	fsPath := pm.sourceRootSlash + rel

	data, err := os.ReadFile(fsPath)
	if err != nil {
		pm.cache.parsed.Set(vfsPath, nil)

		return nil
	}

	out := includeDirectiveParsers.parserFor(rel).Parse(rel, data)
	pm.cache.parsed.Set(vfsPath, out)

	return out
}

// parsedIncludes is the flat parser-VFS view consumed by the scanner:
// SOURCE-rooted paths expose the source parser's `local` bucket;
// BUILD-rooted paths expose only what emitters registered in buildParsed.
func (pm *includeParserManager) parsedIncludes(vfsPath VFS) []includeDirective {
	if vfsPath.IsBuild() {
		if parsed, ok := pm.buildParsed[vfsPath.Rel]; ok {
			return parsed
		}

		return nil
	}

	return pm.sourceParsedBuckets(vfsPath.Rel).bucket(parsedIncludesLocal)
}

func (pm *includeParserManager) RegisterBuildParsedIncludes(rel string, parsed []includeDirective) {
	pm.buildParsed[rel] = parsed
}

// fileExistsByRel is the inner, rel-keyed existence check.
func (pm *includeParserManager) fileExistsByRel(rel string) bool {
	if cached, ok := pm.cache.exists[rel]; ok {
		pm.cache.existsHits++
		return cached
	}

	pm.cache.existsMisses++

	info, err := os.Stat(pm.sourceRootSlash + rel)
	val := err == nil && !info.IsDir()
	pm.cache.exists[rel] = val

	return val
}

func (pm *includeParserManager) perfStats() parserPerfStats {
	return parserPerfStats{
		parsedHits:   pm.cache.parsedHits,
		parsedMisses: pm.cache.parsedMisses,
		existsHits:   pm.cache.existsHits,
		existsMisses: pm.cache.existsMisses,
		buildParsed:  len(pm.buildParsed),
	}
}
