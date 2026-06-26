package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	scannerStatsEnabled = os.Getenv("SCANNER_STATS") != ""
	perfStatsEnabled    = os.Getenv("YATOOL_PERF_STATS") != ""
)

type IncludeKind int

const (
	includeSystem IncludeKind = iota
	includeQuoted

	includeCythonOptional

	includeCythonModule

	includeCythonName

	includeCythonFallback

	includeCythonSibling
)

type IncludeDirective struct {
	kind   IncludeKind
	target STR
}

func (d IncludeDirective) quotedLike() bool {
	return d.kind == includeQuoted || d.cythonProbe()
}

func (d IncludeDirective) cythonProbe() bool {
	return d.kind == includeCythonOptional || d.kind == includeCythonModule || d.kind == includeCythonName || d.kind == includeCythonFallback || d.kind == includeCythonSibling
}

type IncludeScanner struct {
	sysincl                *SysinclCtx
	parsers                *IncludeParserManager
	subgraphClosures       [][]VFS
	closureArena           *BumpAllocator[VFS]
	scanCache              DenseMap3[STR, []VFS, ClosureRef, bool]
	searchTierFlat         *IntMap[VFS]
	searchTierSeen         BitSet
	sourceUnderCache       *IntMap[VFS]
	childArena             *BumpAllocator[VFS]
	spOut                  []VFS
	resolveOut             []VFS
	tjc                    *TarjanCtx
	dfsActive              BitSet
	visitedIDPool          sync.Pool
	scanCtxPool            sync.Pool
	onWarn                 func(Warn)
	walkClosureCalls       uint64
	subgraphHits           uint64
	subgraphMisses         uint64
	subgraphTainted        uint64
	subgraphSubsumed       uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
	statsCallCount         uint64
	codegen                *CodegenRegistry
	moduleByRef            *DenseMap[NodeRef, *ModuleEmitResult]
}

type ScanCtx struct {
	scanner *IncludeScanner
	cfg     ScanContext
	parser  IncludeDirectiveParser
}

type ClosureRef uint32

func (s *IncludeScanner) cachedChildren(v VFS) ([]VFS, bool) {
	return s.scanCache.get1(STR(v.strID()))
}

func (s *IncludeScanner) putChildren(v VFS, children []VFS) {
	s.scanCache.put1(STR(v.strID()), children)
}

func (s *IncludeScanner) cachedClosure(v VFS) (ClosureRef, bool) {
	return s.scanCache.get2(STR(v.strID()))
}

func (s *IncludeScanner) putClosure(v VFS, ref ClosureRef) {
	s.scanCache.put2(STR(v.strID()), ref)
}

func (s *IncludeScanner) sourceFileExists(abs VFS) bool {
	key := STR(abs.strID())

	if exists, probed := s.scanCache.get3(key); probed {
		return exists
	}

	v := s.parsers.fs.isFile(srcRootVFS, abs.rel())
	s.scanCache.put3(key, v)

	return v
}

const (
	closureAllocHint = 1 << 13

	closureArenaInitial = closureAllocHint
)

type ScannerPerfStats struct {
	walkClosureCalls       uint64
	subgraphHits           uint64
	subgraphMisses         uint64
	subgraphTainted        uint64
	subgraphSubsumed       uint64
	searchTierHits         uint64
	searchTierMisses       uint64
	resolveSearchPathCalls uint64
}

func newIncludeScannerWith(parsers *IncludeParserManager, sysincl SysInclSet, onWarn func(Warn), tjc *TarjanCtx) *IncludeScanner {
	s := &IncludeScanner{
		sysincl: newSysinclCtx(sysincl),
		parsers: parsers,
		onWarn:  onWarn,

		subgraphClosures: make([][]VFS, 1, 256),
		closureArena:     newBumpAllocator[VFS](closureArenaInitial),
		childArena:       newBumpAllocator[VFS](1 << 12),
		searchTierFlat:   newIntMap[VFS](4096),
		sourceUnderCache: newIntMap[VFS](1 << 16),
		tjc:              tjc,
	}

	s.visitedIDPool.New = func() any {
		return &IdSet{}
	}

	s.scanCtxPool.New = func() any {
		return &ScanCtx{}
	}

	return s
}

type ScanContext struct {
	OwnAddIncl      []VFS
	PeerAddInclSet  []VFS
	BaseSearchPaths []VFS
	OwnerModuleDir  string
	OwnerModuleTag  STR
	cfg             *ScanConfig
}

type ScanConfig struct {
	num uint32
	ri  *CfgResolveIndex
}

func newScanContext(pm *IncludeParserManager, ownAddIncl, peerAddIncl, base []VFS, ownerModuleDir string) ScanContext {
	cfg := ScanContext{
		OwnAddIncl:      ownAddIncl,
		PeerAddInclSet:  peerAddIncl,
		BaseSearchPaths: base,
		OwnerModuleDir:  ownerModuleDir,
	}
	cfg.cfg = pm.resolveScanConfig(&cfg)

	return cfg
}

func (s *IncludeScanner) getScanCtx(cfg ScanContext, parser IncludeDirectiveParser) *ScanCtx {
	sc := s.scanCtxPool.Get().(*ScanCtx)
	sc.scanner = s
	sc.cfg = cfg
	sc.parser = parser

	return sc
}

func (s *IncludeScanner) putScanCtx(sc *ScanCtx) {
	s.scanCtxPool.Put(sc)
}

func hashScanContext(ctx *ScanContext) uint64 {
	h := uint64(0x9e3779b97f4a7c15)

	mixSlice := func(ss []VFS) {
		h = mix64(h ^ uint64(len(ss)))

		for _, v := range ss {
			h = mix64(h ^ internTable.los[v.strID()])
		}
	}

	mixSlice(ctx.OwnAddIncl)
	mixSlice(ctx.PeerAddInclSet)
	mixSlice(ctx.BaseSearchPaths)

	return h
}

func (s *IncludeScanner) parsedIncludes(vfsPath VFS, ctxParser IncludeDirectiveParser) []IncludeDirective {
	if vfsPath.isBuild() {
		return s.codegen.buildParsedFor(vfsPath)
	}

	return s.parsers.sourceParsedBuckets(vfsPath, ctxParser).bucket(s.parsers.registry.walkableBucketFor(vfsPath.rel()))
}

func (sc *ScanCtx) forEachResolvedChild(vfsPath VFS, fn func(rabs VFS)) {
	s := sc.scanner
	incDir := dirKey(pathDir(vfsPath.rel()))

	suppressCimportNames := false
	prevProbeMissed := false

	for _, entry := range s.parsedIncludes(vfsPath, sc.parser) {
		if entry.kind == includeCythonSibling {
			for _, rabs := range sc.resolve(vfsPath, incDir, entry) {
				fn(rabs)
			}

			continue
		}

		isName := entry.kind == includeCythonName
		isFallback := entry.kind == includeCythonFallback

		if !isName && !isFallback {
			suppressCimportNames = false
		}

		if (isName && suppressCimportNames) || (isFallback && !prevProbeMissed) {
			prevProbeMissed = false

			continue
		}

		resolved := sc.resolve(vfsPath, incDir, entry)

		for _, rabs := range resolved {
			fn(rabs)
		}

		prevProbeMissed = len(resolved) == 0

		if entry.kind == includeCythonModule && len(resolved) > 0 {
			suppressCimportNames = true
		}
	}

	sc.resolveInducedDeps(vfsPath, incDir, fn)
}

func (sc *ScanCtx) resolveInducedDeps(vfsPath VFS, incDir VFS, fn func(rabs VFS)) {
	s := sc.scanner

	if !vfsPath.isBuild() {
		return
	}

	info := s.codegen.lookup(vfsPath)

	if info == nil {
		return
	}

	bucket := parsedIncludesCpp

	if isHeaderSource(vfsPath.rel()) {
		bucket = parsedIncludesHeader
	}

	for _, gref := range info.GeneratorRefs {
		tool, ok := s.moduleByRef.get(gref)

		if !ok {
			continue
		}

		for _, entry := range tool.InducedDeps.bucket(bucket) {
			for _, rabs := range sc.resolve(vfsPath, incDir, entry) {
				fn(rabs)
			}
		}
	}
}

func (sc *ScanCtx) forEachResolvedChildID(abs VFS, fn func(VFS)) {
	s := sc.scanner

	if cached, ok := s.cachedChildren(abs); ok {
		for _, id := range cached {
			fn(id)
		}

		return
	}

	block := s.childArena.alloc(closureAllocHint)
	k := 0
	sc.forEachResolvedChild(abs, func(rabs VFS) {
		block[k] = rabs
		k++
	})
	s.childArena.commit(k)

	var children []VFS

	if k > 0 {
		children = block[:k]
	}

	s.putChildren(abs, children)

	for _, id := range children {
		fn(id)
	}
}

func (s *IncludeScanner) subgraphCacheStats() (hits, misses, tainted uint64) {
	return s.subgraphHits, s.subgraphMisses, s.subgraphTainted
}

func (s *IncludeScanner) perfStats() ScannerPerfStats {
	return ScannerPerfStats{
		walkClosureCalls:       s.walkClosureCalls,
		subgraphHits:           s.subgraphHits,
		subgraphMisses:         s.subgraphMisses,
		subgraphTainted:        s.subgraphTainted,
		subgraphSubsumed:       s.subgraphSubsumed,
		searchTierHits:         s.searchTierHits,
		searchTierMisses:       s.searchTierMisses,
		resolveSearchPathCalls: s.resolveSearchPathCalls,
	}
}

func (sc *ScanCtx) dfs(abs VFS) {
	s := sc.scanner

	if s.dfsActive.has(uint32(abs)) {
		s.subgraphHits += s.tjc.runSCC(sc, abs)

		return
	}

	s.dfsActive.add(uint32(abs))

	sc.forEachResolvedChildID(abs, func(ch VFS) {
		if ch == abs {
			return
		}

		sc.closureOf(ch)
	})

	s.tjc.closure.reset(vfsBound())

	block := s.closureArena.alloc(closureAllocHint)
	k := 0

	s.tjc.closure.add(abs)
	block[k] = abs
	k++

	sc.forEachResolvedChildID(abs, func(ch VFS) {
		if ch == abs {
			return
		}

		if sc.windowSubsumed(ch) {
			return
		}

		cref, _ := s.cachedClosure(ch)
		k = s.tjc.closure.spliceNew(s.closureWindow(cref), block, k)
	})

	for i := 0; i < k; i++ {
		if block[i].isBuild() {
			k = s.tjc.closure.spliceNew(s.codegen.closureLeaves(block[i]), block, k)
		}
	}

	s.closureArena.commit(k)
	ref := ClosureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k])
	s.putClosure(abs, ref)
}

func (sc *ScanCtx) closureOf(abs VFS) []VFS {
	s := sc.scanner

	ref, ok := s.cachedClosure(abs)

	if ok {
		s.subgraphHits++
	} else {
		sc.dfs(abs)

		ref, _ = s.cachedClosure(abs)
	}

	w := s.subgraphClosures[ref]

	return w
}

func (s *IncludeScanner) closureWindow(ref ClosureRef) []VFS {
	return s.subgraphClosures[ref]
}

func (sc *ScanCtx) windowSubsumed(ch VFS) bool {
	s := sc.scanner

	if !s.tjc.closure.has(ch) {
		return false
	}

	if s.codegen.isLeafEver(ch) {
		return false
	}

	s.subgraphSubsumed++

	return true
}

func (sc *ScanCtx) forEachChild(v VFS, fn func(VFS)) {
	sc.forEachResolvedChildID(v, fn)
}

func (sc *ScanCtx) cachedWindow(v VFS) ([]VFS, bool) {
	ref, ok := sc.scanner.cachedClosure(v)

	if !ok {
		return nil, false
	}

	return sc.scanner.closureWindow(ref), true
}

func (sc *ScanCtx) emitClosure(members []VFS, fill func(block []VFS) int) {
	s := sc.scanner

	block := s.closureArena.alloc(closureAllocHint)
	k := fill(block)

	for i := 0; i < k; i++ {
		if block[i].isBuild() {
			k = s.tjc.closure.spliceNew(s.codegen.closureLeaves(block[i]), block, k)
		}
	}

	s.closureArena.commit(k)

	ref := ClosureRef(len(s.subgraphClosures))
	s.subgraphClosures = append(s.subgraphClosures, block[:k])

	s.subgraphMisses += uint64(len(members))

	if len(members) > 1 {
		s.subgraphTainted++
	}

	for _, u := range members {
		s.putClosure(u, ref)
	}
}

func (sc *ScanCtx) resolve(includerAbs, incDir VFS, d IncludeDirective) (out []VFS) {
	s := sc.scanner

	if v := d.target.vfs(); v != 0 {
		out = append(s.resolveOut[:0], v)
		s.resolveOut = out

		return out
	}

	var sysinclClaimed bool

	defer func() {
		if d.cythonProbe() {
			return
		}

		if len(out) > 0 || sysinclClaimed {
			return
		}

		open, close := `<`, `>`

		if d.kind == includeQuoted {
			open, close = `"`, `"`
		}

		s.onWarn(Warn{
			Kind: WarnMissingInclude,
			Message: fmt.Sprintf("%s: unresolved include %s%s%s — not found in source, build, search path, or sysincl",
				includerAbs.string(), open, d.target.string(), close),
		})
	}()

	searchOut := sc.resolveSearchPath(includerAbs, incDir, d)

	includerRel := includerAbs.rel()
	var mappings []VFS
	var hasMultiTarget bool
	mappings, hasMultiTarget, sysinclClaimed = s.sysincl.lookup(includerRel, d.target)

	if d.quotedLike() && len(searchOut) > 0 {
		bypass := !hasMultiTarget

		if !bypass && searchOut[0].isSource() {
			incDir := pathDir(includerRel)

			var sameDirRel string

			if incDir != "" {
				sameDirRel = incDir + "/" + d.target.string()
			} else {
				sameDirRel = d.target.string()
			}

			bypass = searchOut[0].rel() == sameDirRel
		}

		if bypass {
			return searchOut
		}
	}

	if len(mappings) == 0 {
		return searchOut
	}

	if len(searchOut) == 0 {
		res := s.resolveOut[:0]

	fastLoop:
		for _, abs := range mappings {
			for _, q := range res {
				if q == abs {
					continue fastLoop
				}
			}

			if !s.sourceFileExists(abs) {
				continue
			}

			res = append(res, abs)
		}

		s.resolveOut = res

		if len(res) == 0 {
			return nil
		}

		out = res

		return out
	}

	merged := s.resolveOut[:0]
	added := false

mapLoop:
	for _, abs := range mappings {
		base := searchOut

		if added {
			base = merged
		}

		for _, q := range base {
			if q == abs {
				continue mapLoop
			}
		}

		if !s.sourceFileExists(abs) {
			continue
		}

		if !added {
			merged = append(merged, searchOut...)
			added = true
		}

		merged = append(merged, abs)
	}

	s.resolveOut = merged

	if !added {
		return searchOut
	}

	out = merged

	return out
}

type CfgResolveIndex struct {
	indexable    bool
	rank         *IntValueMap[int32]
	buildEntries []CfgBuildAddincl
}

type CfgBuildAddincl struct {
	prefix    VFS
	prefixSrc VFS
	rank      int
}

const resolveNoRank = int(^uint(0) >> 1)

func buildCfgResolveIndex(cfg *ScanContext) *CfgResolveIndex {
	idx := &CfgResolveIndex{}

	for _, p := range cfg.OwnAddIncl {
		if p.isSource() && p.rel() == "" {
			return idx
		}
	}

	for _, p := range cfg.PeerAddInclSet {
		if p.isSource() && p.rel() == "" {
			return idx
		}
	}

	idx.indexable = true
	idx.rank = newIntValueMap[int32](2 * (len(cfg.OwnAddIncl) + len(cfg.PeerAddInclSet)))

	deduper.reset()

	r := int32(0)
	add := func(p VFS) {
		if !deduper.add(p) {
			return
		}

		idx.rank.put(uint64(p), r)

		if p.isBuild() {
			idx.buildEntries = append(idx.buildEntries, CfgBuildAddincl{
				prefix:    p,
				prefixSrc: source(p.rel()),
				rank:      int(r),
			})
		}

		r++
	}

	for _, p := range cfg.OwnAddIncl {
		add(p)
	}

	for _, p := range cfg.PeerAddInclSet {
		add(p)
	}

	return idx
}

func (sc *ScanCtx) cacheSearchTier(targetID STR, out VFS) VFS {
	s := sc.scanner
	s.searchTierFlat.put(splitMix64(sc.cfg.cfg.num, uint32(targetID)), out)
	s.searchTierSeen.add(uint32(targetID))

	return out
}

func (sc *ScanCtx) resolveContextSearchTier(targetID STR) VFS {
	s := sc.scanner

	if s.searchTierSeen.has(uint32(targetID)) {
		if cached := s.searchTierFlat.get(splitMix64(sc.cfg.cfg.num, uint32(targetID))); cached != nil {
			s.searchTierHits++

			return *cached
		}
	}

	s.searchTierMisses++

	target := targetID.string()

	var out VFS

	normTarget := normalisePath(target)

	addSource := func(prefix VFS) bool {
		v := s.resolveSourceUnder(prefix, target)

		if v == 0 {
			return false
		}

		out = v

		return true
	}

	buildSuffix := interned(normTarget)

	addBuild := func(prefixRel string) bool {
		if buildSuffix == 0 {
			return false
		}

		var info *GeneratedFileInfo

		if prefixRel == "" {
			info = s.codegen.lookupSTR(buildSuffix)
		} else if pid := internedPrefixed("$(S)/", prefixRel); pid != 0 {
			info = s.codegen.lookupSplit(pid.vfs(), buildSuffix)
		}

		if info == nil {
			return false
		}

		out = info.OutputPath

		return true
	}

	addInclPath := func(prefix VFS) bool {
		if prefix.isBuild() {
			return addBuild(prefix.rel())
		}

		return addSource(prefix)
	}

	if addInclPath(bld) || addInclPath(v) {
		return sc.cacheSearchTier(targetID, out)
	}

	first, _ := firstComponent(target)

	if canRelFilter(first, target) && !strings.Contains(target, "/./") && !strings.Contains(target, "//") {
		idx := sc.cfg.cfg.ri

		if idx.indexable {
			bestRank := resolveNoRank
			var bestAddincl VFS

			cands, _ := s.parsers.addinclIndex.get(targetID)

			for _, a := range cands {
				if rp := idx.rank.get(uint64(a)); rp != nil && int(*rp) < bestRank {
					bestRank = int(*rp)
					bestAddincl = a
				}
			}

			bestIsSource := bestRank != resolveNoRank

			var bestBuild *GeneratedFileInfo

			if buildSuffix != 0 {
				for i := range idx.buildEntries {
					b := &idx.buildEntries[i]

					if b.rank >= bestRank {
						continue
					}

					info := s.codegen.lookupSplit(b.prefixSrc, buildSuffix)

					if info == nil {
						continue
					}

					bestRank = b.rank
					bestBuild = info
					bestIsSource = false
				}
			}

			if bestRank != resolveNoRank {
				if bestIsSource {
					out = sourceJoined(bestAddincl.rel(), target)
				} else {
					out = bestBuild.OutputPath
				}

				return sc.cacheSearchTier(targetID, out)
			}

			for _, p := range sc.cfg.BaseSearchPaths {
				if addInclPath(p) {
					return sc.cacheSearchTier(targetID, out)
				}
			}

			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.OwnAddIncl {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.PeerAddInclSet {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.BaseSearchPaths {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	return sc.cacheSearchTier(targetID, out)
}

func (sc *ScanCtx) resolveSearchPath(includerAbs, incDir VFS, d IncludeDirective) []VFS {
	s := sc.scanner
	s.resolveSearchPathCalls++

	out := s.spOut[:0]

	defer func() {
		s.spOut = out[:0]
	}()

	outHas := func(v VFS) bool {
		for _, x := range out {
			if x == v {
				return true
			}
		}

		return false
	}

	addPath := func(rel string) bool {
		rel = normalisePath(rel)

		if !s.parsers.fs.isFile(srcRootVFS, rel) {
			return false
		}

		v := source(rel)

		if outHas(v) {
			return false
		}

		out = append(out, v)

		return true
	}

	searchPathFound := false

	if candidate, ok := cythonPy2SiblingOverride(includerAbs, d); ok && addPath(candidate) {
		searchPathFound = true
	}

	if includerAbs.isBuild() {
		if info := s.codegen.lookupSTR(d.target); info != nil && !outHas(info.OutputPath) {
			out = append(out, info.OutputPath)
			searchPathFound = true
		}
	}

	if d.quotedLike() {
		matched := false

		suKey := splitMix64(uint32(incDir), uint32(d.target))
		var sv VFS

		if p := s.sourceUnderCache.get(suKey); p != nil {
			sv = *p
		} else {
			sv = s.resolveSourceUnder(incDir, d.target.string())
			s.sourceUnderCache.put(suKey, sv)
		}

		if sv != 0 {
			out = append(out, sv)
			searchPathFound = true
			matched = true
		}

		if !matched {
			if info := s.codegen.lookupSplit(incDir, d.target); info != nil {
				if !outHas(info.OutputPath) {
					out = append(out, info.OutputPath)
					searchPathFound = true
				}
			}
		}
	}

	if !searchPathFound {
		tier := sc.resolveContextSearchTier(d.target)

		if tier != 0 {
			out = append(out, tier)
			searchPathFound = true
		}
	}

	return out
}

func cythonPy2SiblingOverride(includerAbs VFS, d IncludeDirective) (string, bool) {
	if !includerAbs.isSource() || !d.quotedLike() {
		return "", false
	}

	if hasPrefix(includerAbs.rel(), "contrib/tools/cython_py2/Cython/Includes/") {
		if hasPrefix(d.target.string(), "libc/") || hasPrefix(d.target.string(), "libcpp/") {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
		}

		return "", false
	}

	switch includerAbs.rel() {
	case "util/generic/string.pxd":
		if d.target.string() == "libcpp/string.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
		}
	case "util/generic/hash.pxd", "util/generic/hash_set.pxd":
		if d.target.string() == "libcpp/pair.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
		}
	case "util/system/types.pxd":
		if d.target.string() == "libc/stdint.pxd" {
			return "contrib/tools/cython_py2/Cython/Includes/" + d.target.string(), true
		}
	}

	return "", false
}

func pathDir(p string) string {
	idx := strings.LastIndexByte(p, '/')

	if idx < 0 {
		return ""
	}

	return p[:idx]
}

func normalisePath(p string) string {
	if !strings.Contains(p, "..") && !strings.Contains(p, "./") && !strings.Contains(p, "//") {
		return p
	}

	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))

	for _, seg := range parts {
		switch seg {
		case "", ".":

			continue
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}

	return strings.Join(out, "/")
}

func (s *IncludeScanner) resolveSourceUnder(prefix VFS, target string) VFS {
	if !s.parsers.fs.isFile(prefix, target) {
		return 0
	}

	if target != "" && pathIsClean(target) {
		return sourceJoined(prefix.rel(), target)
	}

	return source(normalisePath(joinRel(prefix.rel(), target)))
}

func canRelFilter(first, target string) bool {
	return first != "" && first != "." && first != ".." && !strings.Contains(target, "/..")
}
