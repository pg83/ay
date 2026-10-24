package main

import (
	"fmt"
	"strings"
	"sync"
)

const (
	closureAllocHint  = 1 << 13
	resolveNoRank     = int(^uint(0) >> 1)
	searchTierHotMask = 1<<15 - 1
)

const (
	includeSystem IncludeKind = iota
	includeRos
	includeQuoted

	includeCythonOptional

	includeCythonModule

	includeCythonName

	includeCythonFallback

	includeCythonSibling
	includeTypeScript
	includeMacro
)

const (
	scanDomainCC ScanDomain = iota
	scanDomainAsm
	scanDomainCython
	scanDomainProto
	scanDomainAux
	scanDomainFlatc
	scanDomainRos
	scanDomainGoAsm
	scanDomainSwig
	scanDomainSc
	scanDomainCount
)

type IncludeKind int

type IncludeDirective struct {
	kind   IncludeKind
	target ANY
}

func includeTarget(s ANY) ANY {
	if s.vfs() != 0 {
		return s
	}

	return pathAny(s.str())
}

func (d IncludeDirective) quotedLike() bool {
	return d.kind == includeQuoted || d.cythonProbe()
}

func (d IncludeDirective) cythonProbe() bool {
	return d.kind == includeCythonOptional || d.kind == includeCythonModule || d.kind == includeCythonName || d.kind == includeCythonFallback || d.kind == includeCythonSibling
}

type IncludeScanner struct {
	sysincl        *SysinclCtx
	parsers        *IncludeParserManager
	buckets        *BucketCache
	closureScratch []VFS
	scanCache      DenseMap2[VFS, ScannedChildren, Closure]
	searchTierFlat *IntMap[VFS]
	searchTierSeen BitSet
	searchTierHot  [searchTierHotMask + 1]searchTierHotEntry
	childArena     *BumpAllocator[VFS]
	dfsActive      BitSet
	scanCtxPool    sync.Pool
	onWarn         func(Warn)
	codegen        *CodegenRegistry
	moduleByRef    *DenseMap[NodeRef, *ModuleEmitResult]
	noResolve      bool
	unresolved     *IncludeScanner
	asmContexts    map[uint32]*IncludeScanner
}

type searchTierHotEntry struct {
	key uint64
	val VFS
}

type IncludeDiagnostic struct {
	Warning   Warn
	Directive IncludeDirective
}

type IncludeDiagnostics struct {
	Entries []IncludeDiagnostic
}

type ScannedChildren struct {
	Paths       []VFS
	Diagnostics *IncludeDiagnostics
}

type ScanCtx struct {
	diagnostics []IncludeDiagnostic
	scanner     *IncludeScanner
	cfg         *ScanConfig
	ccCfg       *ScanConfig
	cythonCfg   *ScanConfig
	protoCfg    *ScanConfig
	scCfg       *ScanConfig
	parser      IncludeDirectiveParser
	tjc         *TarjanCtx
	tsImports   bool
}

func (s *IncludeScanner) closure(v VFS) (Closure, bool) {
	return s.scanCache.get2(v)
}

func (s *IncludeScanner) putClosure(v VFS, cl Closure) {
	s.scanCache.put2(v, cl)
}

func (s *IncludeScanner) cachedChildren(v VFS) ([]VFS, bool) {
	entry, ok := s.scanCache.get1(v)

	return entry.Paths, ok
}

func (s *IncludeScanner) putChildren(v VFS, children []VFS, diagnostics []IncludeDiagnostic) ScannedChildren {
	entry := ScannedChildren{Paths: children}

	if len(diagnostics) > 0 {
		entry.Diagnostics = &IncludeDiagnostics{Entries: diagnostics}
	}

	s.scanCache.put1(v, entry)

	return entry
}

func (s *IncludeScanner) hasOwnDiagnostics(v VFS) bool {
	entry, _ := s.scanCache.get1(v)

	return entry.Diagnostics != nil
}

func (s *IncludeScanner) reportDiagnostics(diagnostics *IncludeDiagnostics) {
	if diagnostics != nil {
		for _, diagnostic := range diagnostics.Entries {
			s.onWarn(diagnostic.Warning)
		}
	}
}

func (s *IncludeScanner) reportClosureDiagnostics(input VFS, cl Closure) {
	if !cl.hasDiagnostics {
		return
	}

	info := s.codegen.lookup(input)

	cl.each(func(v VFS) {
		entry, _ := s.scanCache.get1(v)

		if entry.Diagnostics == nil {
			return
		}

		for _, diagnostic := range entry.Diagnostics.Entries {
			if info.providesSourceInclude(v, diagnostic.Directive) {
				continue
			}

			s.onWarn(diagnostic.Warning)
		}
	})
}

func newIncludeScannerWith(parsers *IncludeParserManager, sysincl SysInclSet, onWarn func(Warn), buckets *BucketCache) *IncludeScanner {
	s := &IncludeScanner{
		sysincl: newSysinclCtx(sysincl),
		parsers: parsers,
		onWarn:  onWarn,

		buckets:        buckets,
		closureScratch: make([]VFS, closureAllocHint),
		childArena:     newBumpAllocator[VFS](),
		searchTierFlat: newIntMap[VFS](1 << 16),
	}

	s.scanCtxPool.New = func() any {
		return &ScanCtx{}
	}

	return s
}

func (s *IncludeScanner) withoutIncludeResolution() *IncludeScanner {
	if s.unresolved == nil {
		u := newIncludeScannerWith(s.parsers, nil, s.onWarn, s.buckets)

		u.sysincl = s.sysincl
		u.codegen = s.codegen
		u.moduleByRef = s.moduleByRef
		u.noResolve = true

		s.unresolved = u
	}

	return s.unresolved
}

func (s *IncludeScanner) assemblerContext(ctx *ScanContext) *IncludeScanner {
	key := ctx.config(scanDomainAsm).num

	if scanner := s.asmContexts[key]; scanner != nil {
		return scanner
	}

	scanner := newIncludeScannerWith(s.parsers, nil, s.onWarn, s.buckets)

	scanner.sysincl = s.sysincl
	scanner.codegen = s.codegen
	scanner.moduleByRef = s.moduleByRef
	scanner.noResolve = s.noResolve

	if s.asmContexts == nil {
		s.asmContexts = make(map[uint32]*IncludeScanner)
	}

	s.asmContexts[key] = scanner

	return scanner
}

type ScanDomain uint8

type ScanContext struct {
	configs  [scanDomainCount]*ScanConfig
	base     []VFS
	ccOwn    []VFS
	ccPeer   []VFS
	fullPeer []VFS
	parsers  *IncludeParserManager

	asmAddIncl     []VFS
	cythonAddIncl  []VFS
	cythonPeerIncl []VFS
	protoInclude   []VFS
	rosInclude     []VFS
	swigInclude    []VFS
	scInclude      []VFS
	flatcInclude   []VFS
	protoOutRoot   VFS
	goAsmIncl      []VFS
	cythonSys      []VFS
}

type ScanConfig struct {
	num             uint32
	ownAddIncl      []VFS
	peerAddInclSet  []VFS
	baseSearchPaths []VFS
	ri              *CfgResolveIndex
}

func newScanContext(na *NodeArenas, pm *IncludeParserManager, ccOwn, ccPeer, fullPeer, base []VFS) *ScanContext {
	ctx := na.scanContext()

	*ctx = ScanContext{base: base, ccOwn: ccOwn, ccPeer: ccPeer, fullPeer: fullPeer, parsers: pm}

	return ctx
}

func (ctx *ScanContext) modulePaths(domain ScanDomain) (own, peer []VFS) {
	switch domain {
	case scanDomainAsm:
		return ctx.asmAddIncl, nil
	case scanDomainCython:
		return dedup(ctx.cythonAddIncl, ctx.cythonSys), ctx.cythonPeerIncl
	case scanDomainProto:
		return ctx.parsers.protoScanOwnPaths(ctx.protoOutRoot, ctx.protoInclude), nil
	case scanDomainAux:
		return ctx.ccOwn, ctx.fullPeer
	case scanDomainRos:
		return ctx.rosInclude, nil
	case scanDomainFlatc:
		if len(ctx.flatcInclude) != 0 {
			return dedup(ctx.flatcInclude, ctx.ccOwn), ctx.ccPeer
		}

		return ctx.ccOwn, ctx.ccPeer
	case scanDomainGoAsm:
		return ctx.goAsmIncl, nil
	case scanDomainSwig:
		return ctx.swigInclude, nil
	case scanDomainSc:
		return ctx.scInclude, nil
	default:
		return ctx.ccOwn, ctx.ccPeer
	}
}

func (ctx *ScanContext) config(domain ScanDomain) *ScanConfig {
	cfg := ctx.configs[domain]

	if cfg == nil {
		own, peer := ctx.modulePaths(domain)

		cfg = ctx.parsers.resolveScanConfig(own, peer, ctx.base)
		ctx.configs[domain] = cfg
	}

	return cfg
}

func (s *IncludeScanner) getScanCtx(ctx *ScanContext, domain ScanDomain, parser IncludeDirectiveParser) *ScanCtx {
	sc := s.scanCtxPool.Get().(*ScanCtx)

	sc.scanner = s
	sc.cfg = ctx.config(domain)
	sc.ccCfg = nil
	sc.cythonCfg = ctx.config(scanDomainCython)
	sc.protoCfg = nil
	sc.scCfg = ctx.config(scanDomainSc)
	sc.parser = parser
	sc.tsImports = domain == scanDomainAux

	if domain == scanDomainCC {
		sc.protoCfg = ctx.parsers.resolveScanConfig(ctx.protoInclude, nil, ctx.base)
	}

	if domain == scanDomainSwig || domain == scanDomainSc {
		sc.ccCfg = ctx.config(scanDomainCC)
	}

	sc.tjc = tarjans.get()

	return sc
}

func (s *IncludeScanner) putScanCtx(sc *ScanCtx) {
	sc.diagnostics = nil
	tarjans.put(sc.tjc)
	sc.tjc = nil
	s.scanCtxPool.Put(sc)
}

func hashScanConfig(ownAddIncl, peerAddIncl, baseSearchPaths []VFS) uint64 {
	h := uint64(0x9e3779b97f4a7c15)

	mixSlice := func(ss []VFS) {
		h = mix64(h ^ uint64(len(ss)))

		for _, v := range ss {
			h = mix64(h ^ uint64(v))
		}
	}

	mixSlice(ownAddIncl)
	mixSlice(peerAddIncl)
	mixSlice(baseSearchPaths)

	return h
}

func (s *IncludeScanner) parsedIncludes(vfsPath VFS, ctxParser IncludeDirectiveParser) (own, compileExtra []IncludeDirective, info *GeneratedFileInfo, inducedBucket ParsedIncludeBucket) {
	if vfsPath.isBuild() {
		info = s.codegen.lookup(vfsPath)

		if info == nil {
			return nil, nil, nil, parsedIncludesLocal
		}

		if info.OnUse != nil {
			fireGenerated(info)
		}

		if _, empty := s.parsers.registry.registeredParserFor(vfsPath.relString()).(EmptyIncludeDirectiveParser); empty {
			return nil, nil, info, parsedIncludesLocal
		}

		set := info.ParsedIncludes

		if _, ok := s.parsers.registry.registeredParserFor(vfsPath.relString()).(CIncludeDirectiveParser); ok {
			inducedBucket = parsedIncludesCpp

			if isHeaderSource(vfsPath.relString()) {
				inducedBucket = parsedIncludesHeader
			}

			compileExtra = set.bucket(parsedIncludesCpp)
		}

		return set.bucket(parsedIncludesLocal), compileExtra, info, inducedBucket
	}

	return s.parsers.sourceParsedWalkable(vfsPath, ctxParser), nil, nil, parsedIncludesLocal
}

func (s *IncludeScanner) parsedBucketForInput(vfsPath VFS, sourceBucket ParsedIncludeBucket, ctxParser IncludeDirectiveParser) []IncludeDirective {
	if vfsPath.isBuild() {
		return s.codegen.buildParsedFor(vfsPath).bucket(parsedIncludesLocal)
	}

	return s.parsers.sourceParsedBuckets(vfsPath, ctxParser).bucket(sourceBucket)
}

func (sc *ScanCtx) appendResolvedChildren(out []VFS, vfsPath VFS) []VFS {
	s := sc.scanner
	parser := sc.parser

	if sc.protoCfg != nil {
		switch s.parsers.registry.registeredParserFor(vfsPath.relString()).(type) {
		case ProtoIncludeDirectiveParser, CfgProtoIncludeDirectiveParser:
			cfg := sc.cfg

			sc.cfg = sc.protoCfg

			defer func() { sc.cfg = cfg }()
		}
	}

	if sc.ccCfg != nil && sc.parser != IncludeDirectiveParser(swigParser) {
		cfg := sc.cfg

		sc.cfg = sc.ccCfg
		parser = cParser

		defer func() { sc.cfg = cfg }()
	}

	if _, ok := s.parsers.registry.registeredParserFor(vfsPath.relString()).(CythonIncludeDirectiveParser); ok {
		cfg := sc.cfg

		sc.cfg = sc.cythonCfg

		defer func() { sc.cfg = cfg }()
	}

	if sc.parser == IncludeDirectiveParser(scParser) {
		cfg := sc.cfg

		sc.cfg = sc.scCfg
		parser = scParser

		defer func() { sc.cfg = cfg }()
	}

	own, compileExtra, info, inducedBucket := s.parsedIncludes(vfsPath, parser)

	if len(own) == 0 && len(compileExtra) == 0 && !vfsPath.isBuild() {
		return out
	}

	incDir := internStr(pathDir(vfsPath.relString())).source()
	suppressCimportNames := false
	prevProbeMissed := false
	entries := own

	for pass := 0; pass < 2; pass++ {
		for _, entry := range entries {
			if entry.kind == includeCythonSibling {
				out = sc.resolve(out, vfsPath, incDir, entry)

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

			mark := len(out)

			out = sc.resolve(out, vfsPath, incDir, entry)
			prevProbeMissed = len(out) == mark

			if entry.kind == includeCythonModule && len(out) != mark {
				suppressCimportNames = true
			}
		}

		entries = compileExtra
	}

	return sc.appendResolvedInducedDeps(out, vfsPath, incDir, info, inducedBucket)
}

func (sc *ScanCtx) appendResolvedInducedDeps(out []VFS, vfsPath VFS, incDir VFS, info *GeneratedFileInfo, bucket ParsedIncludeBucket) []VFS {
	s := sc.scanner

	if !vfsPath.isBuild() {
		return out
	}

	if info == nil {
		return out
	}

	out = append(out, info.ClosureLeaves...)

	if len(info.GeneratorRefs) == 0 || bucket == parsedIncludesLocal {
		return out
	}

	for _, gref := range info.GeneratorRefs {
		tool, _ := s.moduleByRef.get(gref)

		if tool == nil {
			continue
		}

		for _, entry := range tool.InducedDeps.bucket(bucket) {
			out = sc.resolve(out, vfsPath, incDir, entry)
		}
	}

	return out
}

func (sc *ScanCtx) resolvedChildren(abs VFS) ScannedChildren {
	s := sc.scanner

	if cached, ok := s.scanCache.get1(abs); ok {
		return cached
	}

	scratch := vfsScratches.get()

	sc.diagnostics = nil
	scratch = sc.appendResolvedChildren(scratch, abs)

	diagnostics := sc.diagnostics

	sc.diagnostics = nil

	k := len(scratch)
	block := s.childArena.alloc(k)

	copy(block, scratch)
	s.childArena.commit(k)
	vfsScratches.put(scratch)

	var children []VFS

	if k > 0 {
		children = block[:k]
	}

	return s.putChildren(abs, children, diagnostics)
}

func (sc *ScanCtx) forEachResolvedChildID(abs VFS, fn func(VFS)) {
	if sc.ccCfg != nil || sc.parser == IncludeDirectiveParser(scParser) || sc.scanner.parsers.registry.registeredParserFor(abs.relString()) == IncludeDirectiveParser(scParser) {
		previous := sc.parser

		if parser := sc.scanner.parsers.registry.registeredParserFor(abs.relString()); parser != nil {
			sc.parser = parser
		} else if cached, ok := sc.scanner.parsers.cache.parsed.get(abs.rel()); ok {
			sc.parser = cached.parser
		}

		defer func() { sc.parser = previous }()
	}

	for _, id := range sc.resolvedChildren(abs).Paths {
		fn(id)
	}
}

func (sc *ScanCtx) dfs(abs VFS) {
	s := sc.scanner

	if sc.ccCfg != nil || sc.parser == IncludeDirectiveParser(scParser) || sc.scanner.parsers.registry.registeredParserFor(abs.relString()) == IncludeDirectiveParser(scParser) {
		previous := sc.parser

		if parser := s.parsers.registry.registeredParserFor(abs.relString()); parser != nil {
			sc.parser = parser
		} else if cached, ok := s.parsers.cache.parsed.get(abs.rel()); ok {
			sc.parser = cached.parser
		}

		defer func() { sc.parser = previous }()
	}

	if s.dfsActive.has(uint32(abs)) {
		sc.tjc.runSCC(sc, abs)

		return
	}

	s.dfsActive.add(uint32(abs))

	resolved := sc.resolvedChildren(abs)
	children := resolved.Paths
	hasDiagnostics := resolved.Diagnostics != nil

	for _, ch := range children {
		if ch == abs {
			continue
		}

		sc.ensureClosure(ch)
	}

	sc.tjc.closure.reset(vfsBound())
	sc.tjc.closure.add(abs)
	s.buckets.resetScratch(abs)

	for _, input := range sc.opaqueInputs(abs) {
		s.buckets.spliceOne(&sc.tjc.closure, input)
	}

	for _, ch := range children {
		if ch == abs {
			continue
		}

		if sc.tjc.closure.has(ch) {
			continue
		}

		cl, _ := s.closure(ch)

		hasDiagnostics = hasDiagnostics || cl.hasDiagnostics

		s.buckets.spliceOne(&sc.tjc.closure, cl.self)

		for _, bucket := range cl.bucketList() {
			s.buckets.spliceBucket(&sc.tjc.closure, bucket)
		}

		if main := sc.opaqueMainOut(ch); main != 0 {
			s.buckets.spliceOne(&sc.tjc.closure, main)
		}
	}

	if main := sc.mainOut(abs); main != 0 {
		s.buckets.spliceOne(&sc.tjc.closure, main)
	}

	cl := s.buckets.storeScratch(abs)

	cl.hasDiagnostics = hasDiagnostics
	s.putClosure(abs, cl)
}

func (sc *ScanCtx) opaqueMainOut(v VFS) VFS {
	s := sc.scanner

	if !v.isBuild() || s.parsers.registry.scansIncludes(v.relString()) {
		return 0
	}

	if info := s.codegen.lookup(v); info != nil && info.ProducerMainOut != v {
		return info.ProducerMainOut
	}

	return 0
}

func (sc *ScanCtx) mainOut(v VFS) VFS {
	s := sc.scanner

	if !v.isBuild() {
		return 0
	}

	info := s.codegen.lookup(v)

	if info == nil || info.ProducerMainOut == v || !s.parsers.registry.scansIncludes(v.relString()) {
		return 0
	}

	return info.ProducerMainOut
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
	return sc.tjc.closure.has(ch)
}

func (sc *ScanCtx) forEachChild(v VFS, fn func(VFS)) {
	sc.forEachResolvedChildID(v, fn)
}

func (sc *ScanCtx) opaqueInputs(v VFS) []VFS {
	inputs, _ := sc.scanner.codegen.textTemplates.get(v)

	return inputs
}

func (sc *ScanCtx) hasOwnDiagnostics(v VFS) bool {
	return sc.scanner.hasOwnDiagnostics(v)
}

func (sc *ScanCtx) cachedWindow(v VFS) (Closure, bool) {
	return sc.scanner.closure(v)
}

func (sc *ScanCtx) emitClosure(members []VFS, fill func(block []VFS) (int, bool)) {
	s := sc.scanner

	s.codegen.shareInputLoop(members)

	block := s.closureScratch
	k, hasDiagnostics := fill(block)
	cl := s.buckets.storeBuckets(block[0], block[1:k])

	cl.hasDiagnostics = hasDiagnostics

	for _, u := range members {
		s.putClosure(u, cl)
	}
}

func (sc *ScanCtx) resolve(out []VFS, includerAbs, incDir VFS, d IncludeDirective) []VFS {
	s := sc.scanner
	mark := len(out)

	if s.noResolve && (includerAbs.isSource() || d.target.vfs() == 0) {
		return out
	}

	if d.kind == includeRos {
		if resolved := resolveRosInclude(s.parsers.fs, sc.cfg.ownAddIncl, d.target.string()); resolved != 0 {
			return append(out, resolved)
		}

		return out
	}

	if d.kind == includeTypeScript {
		if !sc.tsImports {
			return out
		}

		if file := s.parsers.resolveTSImport(incDir.relString(), d.target.string()); file != 0 {
			return append(out, file)
		}

		sc.warnUnresolved(includerAbs, d, false)

		return out
	}

	if v := d.target.vfs(); v != 0 {
		return append(out, v)
	}

	if d.kind == includeMacro {
		aliases, _, claimed := s.sysincl.lookup(includerAbs.relString(), d.target.str())

		aliases = append([]VFS(nil), aliases...)

		for _, alias := range aliases {
			rel := alias.relString()

			if len(rel) > 2 && rel[0] == '$' && rel[2] == '/' {
				rel = rel[3:]
			}

			out = sc.resolve(out, includerAbs, incDir, IncludeDirective{kind: includeSystem, target: internStr(rel).any()})
		}

		if len(out) == mark {
			sc.warnUnresolved(includerAbs, d, claimed)
		}

		return out
	}

	out = sc.resolveSearchPath(out, includerAbs, incDir, d)

	searchOut := out[mark:]
	includerRel := includerAbs.relString()

	var mappings []VFS
	var hasMultiTarget bool
	var sysinclClaimed bool
	mappings, hasMultiTarget, sysinclClaimed = s.sysincl.lookup(includerRel, d.target.str())

	if d.quotedLike() && len(searchOut) > 0 {
		bypass := !hasMultiTarget

		if !bypass && searchOut[0].isSource() {
			incDir := pathDir(includerRel)
			rel := searchOut[0].relString()
			t := d.target.string()

			if incDir != "" {
				bypass = len(rel) == len(incDir)+1+len(t) &&
					rel[:len(incDir)] == incDir && rel[len(incDir)] == '/' && rel[len(incDir)+1:] == t
			} else {
				bypass = rel == t
			}
		}

		if bypass {
			return out
		}
	}

	if len(mappings) == 0 {
		if len(searchOut) == 0 {
			sc.warnUnresolved(includerAbs, d, sysinclClaimed)
		}

		return out
	}

	if len(searchOut) == 0 {
	fastLoop:
		for _, abs := range mappings {
			for _, q := range out[mark:] {
				if q == abs {
					continue fastLoop
				}
			}

			if s.parsers.fs.resolveSourceUnder(srcRootRel, abs.rel()) == 0 {
				continue
			}

			out = append(out, abs)
		}

		if len(out) == mark {
			sc.warnUnresolved(includerAbs, d, sysinclClaimed)

			return out
		}

		return out
	}

mapLoop:
	for _, abs := range mappings {
		for _, q := range out[mark:] {
			if q == abs {
				continue mapLoop
			}
		}

		if s.parsers.fs.resolveSourceUnder(srcRootRel, abs.rel()) == 0 {
			continue
		}

		out = append(out, abs)
	}

	return out
}

func (sc *ScanCtx) warnUnresolved(includerAbs VFS, d IncludeDirective, sysinclClaimed bool) {
	if d.cythonProbe() || sysinclClaimed {
		return
	}

	open, close := `<`, `>`

	if d.kind == includeQuoted || d.kind == includeMacro {
		open, close = `"`, `"`
	}

	warning := Warn{
		Kind: WarnMissingInclude,
		Message: fmt.Sprintf("%s: unresolved include %s%s%s — not found in source, build, search path, or sysincl",
			includerAbs.string(), open, d.target.string(), close),
	}

	sc.diagnostics = append(sc.diagnostics, IncludeDiagnostic{Warning: warning, Directive: d})
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

func buildCfgResolveIndex(cfg *ScanConfig) *CfgResolveIndex {
	idx := &CfgResolveIndex{}

	for _, p := range cfg.ownAddIncl {
		if p.isSource() && p.relString() == "" {
			return idx
		}
	}

	for _, p := range cfg.peerAddInclSet {
		if p.isSource() && p.relString() == "" {
			return idx
		}
	}

	idx.indexable = true
	idx.rank = newIntValueMap[int32](2 * (len(cfg.ownAddIncl) + len(cfg.peerAddInclSet)))

	r := int32(0)

	add := func(p VFS) {
		if idx.rank.get(uint64(p)) != nil {
			return
		}

		idx.rank.put(uint64(p), r)

		if p.isBuild() {
			idx.buildEntries = append(idx.buildEntries, CfgBuildAddincl{
				prefix:    p,
				prefixSrc: p.rel().source(),
				rank:      int(r),
			})
		}

		r++
	}

	for _, p := range cfg.ownAddIncl {
		add(p)
	}

	for _, p := range cfg.peerAddInclSet {
		add(p)
	}

	return idx
}

func (sc *ScanCtx) cacheSearchTier(targetID STR, out VFS) VFS {
	s := sc.scanner
	key := splitMix64(sc.cfg.num, uint32(targetID))

	s.searchTierFlat.put(key, out)
	s.searchTierSeen.add(uint32(targetID))
	s.searchTierHot[key&searchTierHotMask] = searchTierHotEntry{key: key, val: out}

	return out
}

func (sc *ScanCtx) resolveContextSearchTier(targetID STR) VFS {
	s := sc.scanner
	key := splitMix64(sc.cfg.num, uint32(targetID))
	hot := &s.searchTierHot[key&searchTierHotMask]

	if hot.key == key {
		return hot.val
	}

	if s.searchTierSeen.has(uint32(targetID)) {
		if cached := s.searchTierFlat.get(key); cached != nil {
			hot.key = key
			hot.val = *cached

			return *cached
		}
	}

	target := targetID.string()

	var out VFS

	normTarget := normalisePath(target)
	cleanTarget := normTarget == target

	addSource := func(prefix VFS) bool {
		rel := s.parsers.fs.resolveSourceUnderClean(prefix.rel(), targetID, cleanTarget)

		if rel == 0 {
			return false
		}

		out = rel.source()

		return true
	}

	buildSuffix := targetID

	if !cleanTarget {
		buildSuffix = interned(normTarget)
	}

	addBuild := func(prefix VFS) bool {
		if buildSuffix == 0 {
			return false
		}

		var info *GeneratedFileInfo

		if prefix == bldRootDirVFS {
			info = s.codegen.lookupSTR(buildSuffix)
		} else {
			info = s.codegen.lookupSplit(prefix.rel().source(), buildSuffix)
		}

		if info == nil {
			return false
		}

		out = info.OutputPath

		return true
	}

	addInclPath := func(prefix VFS) bool {
		if prefix.isBuild() {
			return addBuild(prefix)
		}

		return addSource(prefix)
	}

	if buildSuffix != 0 {
		if info := s.codegen.lookupSTR(buildSuffix); info != nil {
			return sc.cacheSearchTier(targetID, info.OutputPath)
		}
	}

	if rel := s.parsers.fs.resolveSourceUnderClean(srcRootRel, targetID, cleanTarget); rel != 0 {
		return sc.cacheSearchTier(targetID, rel.source())
	}

	canIndex := cleanTarget

	if !canIndex {
		first, _ := firstComponent(target)

		canIndex = canRelFilter(first, target) && !strings.Contains(target, "/./") && !strings.Contains(target, "//")
	}

	if canIndex {
		idx := sc.cfg.ri

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
					out = sourceJoined(bestAddincl.relString(), target)
				} else {
					out = bestBuild.OutputPath
				}

				return sc.cacheSearchTier(targetID, out)
			}

			for _, p := range sc.cfg.baseSearchPaths {
				if addInclPath(p) {
					return sc.cacheSearchTier(targetID, out)
				}
			}

			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.ownAddIncl {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.peerAddInclSet {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	for _, p := range sc.cfg.baseSearchPaths {
		if addInclPath(p) {
			return sc.cacheSearchTier(targetID, out)
		}
	}

	return sc.cacheSearchTier(targetID, out)
}

func (sc *ScanCtx) resolveSearchPath(out []VFS, includerAbs, incDir VFS, d IncludeDirective) []VFS {
	s := sc.scanner
	mark := len(out)
	targetID := d.target.str()
	quoted := d.quotedLike()

	outHas := func(v VFS) bool {
		for _, x := range out[mark:] {
			if x == v {
				return true
			}
		}

		return false
	}

	searchPathFound := false

	if includerAbs.isBuild() {
		if info := s.codegen.lookupSTR(targetID); info != nil && !outHas(info.OutputPath) {
			out = append(out, info.OutputPath)
			searchPathFound = true
		}
	}

	if quoted {
		sv := s.parsers.fs.resolveSourceUnder(incDir.rel(), targetID)

		if sv != 0 {
			out = append(out, sv.source())
			searchPathFound = true
		} else if includerAbs.isBuild() {
			if info := s.codegen.lookupSplit(incDir, targetID); info != nil {
				if !outHas(info.OutputPath) {
					out = append(out, info.OutputPath)
					searchPathFound = true
				}
			}
		}
	}

	if !searchPathFound {
		tier := sc.resolveContextSearchTier(targetID)

		if tier != 0 {
			out = append(out, tier)
			searchPathFound = true
		}
	}

	return out
}

func canRelFilter(first, target string) bool {
	return first != "" && first != "." && first != ".." && !strings.Contains(target, "/..")
}

func quotedDirectives(headers []VFS) []IncludeDirective {
	out := make([]IncludeDirective, len(headers))

	for i, h := range headers {
		out[i] = IncludeDirective{kind: includeQuoted, target: includeTarget(h.rel().any())}
	}

	return out
}
