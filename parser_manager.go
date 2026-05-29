package main

import (
	"path"
	"strings"
)

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
	out = pm.withCythonSibling(rel, out)
	pm.cache.parsed[vfsPath] = out

	return out
}

// withCythonSibling models Cython's implicit sibling .pxd: a .pyx uses its
// same-named .pxd only when that file exists. Emitting it unconditionally (as a
// pure parser must, lacking FS access) yields a spurious unresolved include
// whenever the sibling is absent; gate it here, where the FS is available. .pyx
// files are rare, so the extra existence probe is off the hot C/C++ parse path.
func (pm *includeParserManager) withCythonSibling(rel string, set parsedIncludeSet) parsedIncludeSet {
	if !strings.HasSuffix(rel, ".pyx") {
		return set
	}

	sibling := rel[:len(rel)-len(".pyx")] + ".pxd"
	if !pm.fs.IsFile(sibling) {
		return set
	}

	d := includeDirective{kind: includeQuoted, target: internString(path.Base(sibling))}
	local := set.bucket(parsedIncludesLocal)
	merged := make([]includeDirective, 0, 1+len(local))
	merged = append(merged, d)
	merged = append(merged, local...)

	if set == nil {
		set = make(parsedIncludeSet)
	}
	set[parsedIncludesLocal] = merged

	return set
}

func (pm *includeParserManager) parsedIncludes(vfsPath VFS) []includeDirective {
	if vfsPath.IsBuild() {
		if parsed, ok := pm.buildParsed[vfsPath.Rel()]; ok {
			return parsed
		}

		return nil
	}

	rel := vfsPath.Rel()
	if !scannerFollowsImports(rel) {
		return nil
	}

	return pm.sourceParsedBuckets(vfsPath).bucket(parsedIncludesLocal)
}

// scannerFollowsImports reports whether the C/C++ include scanner should expand
// a source file's parsed imports. Proto and SWIG sources have their dependency
// closures modeled by the proto / SWIG codegen (emit_proto, swigIncludeClosure),
// which read sourceParsedBuckets directly. The scanner must not re-walk those
// imports: it lacks the proto/SWIG search roots and would only report spurious
// unresolved includes for deps that already arrive via codegen (verified
// byte-exact: e.g. sg3 google/protobuf/any.proto and swig Lib closures match
// upstream exactly without the scanner's redundant walk).
func scannerFollowsImports(rel string) bool {
	switch directiveParserExt(rel) {
	case ".proto", ".ev", ".gzt", ".gztproto", ".swg":
		return false
	}

	return true
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
