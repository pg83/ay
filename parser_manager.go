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

type parsedIncludeKind uint8

const (
	parsedIncludeDirective parsedIncludeKind = iota
	parsedIncludeDirectVFS
)

type parsedInclude struct {
	kind      parsedIncludeKind
	directive includeDirective
	path      VFS
}

type parsedIncludeSet map[parsedIncludeBucket][]parsedInclude

func rawParsedIncludeSet(bucket parsedIncludeBucket, directives ...includeDirective) parsedIncludeSet {
	if len(directives) == 0 {
		return nil
	}

	out := make([]parsedInclude, 0, len(directives))
	for _, d := range directives {
		out = append(out, parsedInclude{kind: parsedIncludeDirective, directive: d})
	}

	return parsedIncludeSet{bucket: out}
}

func directParsedIncludeSet(bucket parsedIncludeBucket, paths ...VFS) parsedIncludeSet {
	if len(paths) == 0 {
		return nil
	}

	out := make([]parsedInclude, 0, len(paths))
	for _, p := range paths {
		out = append(out, parsedInclude{kind: parsedIncludeDirectVFS, path: p})
	}

	return parsedIncludeSet{bucket: out}
}

func appendParsedDirectives(set parsedIncludeSet, bucket parsedIncludeBucket, directives ...includeDirective) parsedIncludeSet {
	if len(directives) == 0 {
		return set
	}
	if set == nil {
		set = make(parsedIncludeSet)
	}
	for _, d := range directives {
		set[bucket] = append(set[bucket], parsedInclude{kind: parsedIncludeDirective, directive: d})
	}

	return set
}

func appendParsedDirect(set parsedIncludeSet, bucket parsedIncludeBucket, paths ...VFS) parsedIncludeSet {
	if len(paths) == 0 {
		return set
	}
	if set == nil {
		set = make(parsedIncludeSet)
	}
	for _, p := range paths {
		set[bucket] = append(set[bucket], parsedInclude{kind: parsedIncludeDirectVFS, path: p})
	}

	return set
}

func (set parsedIncludeSet) bucket(bucket parsedIncludeBucket) []parsedInclude {
	if set == nil {
		return nil
	}

	return set[bucket]
}

type parsedIncludeLocator interface {
	LookupParsedIncludes(vfsPath VFS) (parsedIncludeSet, bool)
}

type sharedParseCache struct {
	// parsed memoises raw parser results per VFS-rooted path
	// ($(S)/<rel>). 8192 pre-size covers the tools/archiver peak
	// (4354 target + 3559 host, mostly overlapping).
	parsed VFSMap[parsedIncludeSet]
	// exists memoises os.Stat results, keyed by SOURCE_ROOT-relative
	// tail. 16384 covers the observed peak.
	exists map[string]bool
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
}

func newIncludeParserManager(sourceRoot string) *includeParserManager {
	return newIncludeParserManagerWithCache(sourceRoot, newSharedParseCache())
}

func newIncludeParserManagerWithCache(sourceRoot string, cache *sharedParseCache) *includeParserManager {
	return &includeParserManager{
		sourceRoot:      sourceRoot,
		sourceRootSlash: sourceRoot + "/",
		cache:           cache,
	}
}

// scanDirectives returns the raw include directives for the $(S)/-
// rooted file `vfsPath`. The FS translation happens here at the
// os.ReadFile call; raw parsing itself is delegated to a per-extension
// parser from parsers.go. Memoised by VFS path; returns nil for missing
// files (DFS may reach dangling sysincl mappings).
//
// Callers must NOT pass a $(B)/ path — generated outputs are read via
// the CodegenRegistry. IncludeScanner's dispatch layer enforces this.
func (pm *includeParserManager) parsedIncludes(vfsPath VFS, locators []parsedIncludeLocator) parsedIncludeSet {
	if vfsPath.IsBuild() {
		for _, loc := range locators {
			if parsed, ok := loc.LookupParsedIncludes(vfsPath); ok {
				return parsed
			}
		}

		return nil
	}

	if cached, ok := pm.cache.parsed.Get(vfsPath); ok {
		return cached
	}

	fsPath := pm.sourceRootSlash + vfsPath.Rel

	data, err := os.ReadFile(fsPath)
	if err != nil {
		pm.cache.parsed.Set(vfsPath, nil)

		return nil
	}

	out := includeDirectiveParsers.parserFor(vfsPath).Parse(vfsPath, data)
	pm.cache.parsed.Set(vfsPath, out)

	return out
}

func (pm *includeParserManager) scanDirectives(vfsPath VFS, locators []parsedIncludeLocator) []includeDirective {
	parsed := pm.parsedIncludes(vfsPath, locators)
	if len(parsed) == 0 {
		return nil
	}

	entries := parsed.bucket(parsedIncludesLocal)
	if len(entries) == 0 {
		return nil
	}

	out := make([]includeDirective, 0, len(entries))
	for _, entry := range entries {
		if entry.kind != parsedIncludeDirective {
			continue
		}

		out = append(out, entry.directive)
	}

	return out
}

// fileExists is a cached wrapper around os.Stat. Returns true for
// regular files only. Parameter must be $(S)/-rooted — $(B)/ paths
// belong to the codegen registry tier. Cache key is the rel-form tail,
// unified with fileExistsByRel so hot callers skip the `$(S)/` concat.
func (pm *includeParserManager) fileExists(vfsPath VFS) bool {
	return pm.fileExistsByRel(vfsPath.Rel)
}

// fileExistsByRel is the inner, rel-keyed existence check.
func (pm *includeParserManager) fileExistsByRel(rel string) bool {
	if cached, ok := pm.cache.exists[rel]; ok {
		return cached
	}

	info, err := os.Stat(pm.sourceRootSlash + rel)
	val := err == nil && !info.IsDir()
	pm.cache.exists[rel] = val

	return val
}
