package main

import (
	"fmt"
	"strings"
	"sync"
)

const (
	closureAllocHint    = 1 << 13
	closureArenaInitial = closureAllocHint
	resolveNoRank       = int(^uint(0) >> 1)
)

const (
	includeSystem IncludeKind = iota
	includeQuoted

	includeCythonOptional

	includeCythonModule

	includeCythonName

	includeCythonFallback

	includeCythonSibling
)

type IncludeKind int

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
	sysincl          *SysinclCtx
	parsers          *IncludeParserManager
	buckets          *BucketCache
	closureArena     *BumpAllocator[VFS]
	scanCache        DenseMap2[STR, []VFS, Closure]
	searchTierFlat   *IntMap[VFS]
	searchTierSeen   BitSet
	sourceUnderCache *IntMap[VFS]
	childArena       *BumpAllocator[VFS]
	spOut            []VFS
	resolveOut       []VFS
	tjc              *TarjanCtx
	dfsActive        BitSet
	visitedIDPool    sync.Pool
	scanCtxPool      sync.Pool
	onWarn           func(Warn)
	codegen          *CodegenRegistry
	moduleByRef      *DenseMap[NodeRef, *ModuleEmitResult]
}

type ScanCtx struct {
	scanner *IncludeScanner
	cfg     ScanContext
	parser  IncludeDirectiveParser
}

func (s *IncludeScanner) closure(v VFS) (Closure, bool) {
	return s.scanCache.get2(STR(v.strID()))
}

func (s *IncludeScanner) putClosure(v VFS, cl Closure) {
	s.scanCache.put2(STR(v.strID()), cl)
}

func (s *IncludeScanner) cachedChildren(v VFS) ([]VFS, bool) {
	return s.scanCache.get1(STR(v.strID()))
}

func (s *IncludeScanner) putChildren(v VFS, children []VFS) {
	s.scanCache.put1(STR(v.strID()), children)
}

const (
	sourceExistsNo  = 1
	sourceExistsYes = 2
)

func (s *IncludeScanner) sourceFileExists(abs VFS) bool {
	id := abs.strID()

	switch s.parsers.sourceExists.get(id) {
	case sourceExistsYes:
		return true
	case sourceExistsNo:
		return false
	}

	v := s.parsers.fs.isFile(srcRootVFS, abs.rel())

	if v {
		s.parsers.sourceExists.set(id, sourceExistsYes)
	} else {
		s.parsers.sourceExists.set(id, sourceExistsNo)
	}

	return v
}

func newIncludeScannerWith(parsers *IncludeParserManager, sysincl SysInclSet, onWarn func(Warn), tjc *TarjanCtx, buckets *BucketCache) *IncludeScanner {
	s := &IncludeScanner{
		sysincl: newSysinclCtx(sysincl),
		parsers: parsers,
		onWarn:  onWarn,

		buckets:          buckets,
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
			h = mix64(h ^ internTable.flat[v.strID()].lo)
		}
	}

	mixSlice(ctx.OwnAddIncl)
	mixSlice(ctx.PeerAddInclSet)
	mixSlice(ctx.BaseSearchPaths)

	return h
}

func (s *IncludeScanner) parsedIncludes(vfsPath VFS, ctxParser IncludeDirectiveParser) (own, compileExtra []IncludeDirective) {
	if vfsPath.isBuild() {
		set := s.codegen.buildParsedFor(vfsPath)

		return set.bucket(parsedIncludesLocal), set.bucket(parsedIncludesCpp)
	}

	return s.parsers.sourceParsedBuckets(vfsPath, ctxParser).bucket(s.parsers.registry.walkableBucketFor(vfsPath.rel())), nil
}

func (sc *ScanCtx) forEachResolvedChild(vfsPath VFS, fn func(rabs VFS)) {
	s := sc.scanner
	incDir := dirKey(pathDir(vfsPath.rel()))
	suppressCimportNames := false
	prevProbeMissed := false

	process := func(entry IncludeDirective) {
		if entry.kind == includeCythonSibling {
			for _, rabs := range sc.resolve(vfsPath, incDir, entry) {
				fn(rabs)
			}

			return
		}

		isName := entry.kind == includeCythonName
		isFallback := entry.kind == includeCythonFallback

		if !isName && !isFallback {
			suppressCimportNames = false
		}

		if (isName && suppressCimportNames) || (isFallback && !prevProbeMissed) {
			prevProbeMissed = false

			return
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

	own, compileExtra := s.parsedIncludes(vfsPath, sc.parser)

	for _, entry := range own {
		process(entry)
	}

	for _, entry := range compileExtra {
		process(entry)
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

func (sc *ScanCtx) dfs(abs VFS) {
	s := sc.scanner

	if s.dfsActive.has(uint32(abs)) {
		s.tjc.runSCC(sc, abs)

		return
	}

	s.dfsActive.add(uint32(abs))

	sc.forEachResolvedChildID(abs, func(ch VFS) {
		if ch == abs {
			return
		}

		sc.ensureClosure(ch)
	})

	s.tjc.closure.reset(strBound())

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

		cl, _ := s.closure(ch)

		k = cl.spliceInto(&s.tjc.closure, block, k)
	})

	leafStart := k

	if abs.isBuild() {
		k = s.tjc.closure.spliceNew(s.codegen.closureLeaves(abs), block, k)
	}

	for i := leafStart; i < k; i++ {
		if block[i].isBuild() {
			k = s.tjc.closure.spliceNew(s.codegen.closureLeaves(block[i]), block, k)
		}
	}

	s.putClosure(abs, s.buckets.storeBuckets(block[0], block[1:k]))
}

func (sc *ScanCtx) ensureClosure(abs VFS) {
	s := sc.scanner

	if _, ok := s.closure(abs); !ok {
		sc.dfs(abs)
	}
}

func (sc *ScanCtx) closureOf(abs VFS) Closure {
	s := sc.scanner
	cl, ok := s.closure(abs)

	if !ok {
		sc.dfs(abs)

		cl, _ = s.closure(abs)
	}

	return cl
}

func (sc *ScanCtx) windowSubsumed(ch VFS) bool {
	s := sc.scanner

	if !s.tjc.closure.has(ch) {
		return false
	}

	if s.codegen.isLeafEver(ch) {
		return false
	}

	return true
}

func (sc *ScanCtx) forEachChild(v VFS, fn func(VFS)) {
	sc.forEachResolvedChildID(v, fn)
}

func (sc *ScanCtx) cachedWindow(v VFS) (Closure, bool) {
	return sc.scanner.closure(v)
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

	cl := s.buckets.storeBuckets(block[0], block[1:k])

	for _, u := range members {
		s.putClosure(u, cl)
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
		if !deduper.add(p.strID()) {
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
			return *cached
		}
	}

	target := targetID.string()

	var out VFS

	normTarget := normalisePath(target)

	addSource := func(prefix VFS) bool {
		v := s.resolveSourceUnder(prefix, targetID)

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
		sv := s.resolveSourceUnder(incDir, d.target)

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

func (s *IncludeScanner) resolveSourceUnder(prefix VFS, targetSTR STR) VFS {
	key := splitMix64(uint32(prefix), uint32(targetSTR))

	if p := s.sourceUnderCache.get(key); p != nil {
		return *p
	}

	target := targetSTR.string()

	var v VFS

	if s.parsers.fs.isFile(prefix, target) {
		if target != "" && pathIsClean(target) {
			v = sourceJoined(prefix.rel(), target)
		} else {
			v = source(normalisePath(joinRel(prefix.rel(), target)))
		}
	}

	s.sourceUnderCache.put(key, v)

	return v
}

func canRelFilter(first, target string) bool {
	return first != "" && first != "." && first != ".." && !strings.Contains(target, "/..")
}

func quotedDirectives(headers []VFS) []IncludeDirective {
	out := make([]IncludeDirective, len(headers))

	for i, h := range headers {
		out[i] = IncludeDirective{kind: includeQuoted, target: internStr(h.rel())}
	}

	return out
}
