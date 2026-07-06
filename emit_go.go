package main

import (
	"strings"
)

const (
	goStdPrefix    = "contrib/go/_std_1.26/src"
	goArcPrefix    = "a.yandex-team.ru/"
	goVersion      = "1.26"
	goToolsPeer    = "build/external_resources/go_tools"
	goYolintPeer   = "build/external_resources/yolint"
	goVendorPrefix = "vendor"
)

var (
	goKV     = KV{P: pkGO, PC: pcLightRed, ShowOut: true}
	goToolKV = KV{P: pkGoTool, PC: pcLightBlue, ShowOut: true}
	goLdKV   = KV{P: pkLD, PC: pcLightRed, ShowOut: true}

	goVcsEnv = EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	goStdRuntimeVFS = source(goStdPrefix + "/runtime")

	goAsmIncludeDirs = []VFS{
		goStdRuntimeVFS,
		goFakeIncludeVFS,
		contribLibsLinuxHeaders,
		contribLibsLinuxHeadersNf,
	}

	goToolScriptInputsChunk = []VFS{
		source("build/scripts/go_tool.py"),
		buildScriptsProcessCommandFilesPy,
		source("build/scripts/process_whole_archive_option.py"),
		source("build/rules/go/migrations.yaml"),
		source("build/rules/go/extended_lint.yaml"),
		source("build/rules/go/risky_imports.yaml"),
	}

	strGoToolsRoot       = internStr(resourcePatternRef("GO_TOOLS"))
	strGoAsmTool         = internV(resourcePatternRef("GO_TOOLS"), "/pkg/tool/linux_amd64/asm")
	strGoCgoTool         = internV(resourcePatternRef("GO_TOOLS"), "/pkg/tool/linux_amd64/cgo")
	strGoToolsPkgInclude = internV(resourcePatternRef("GO_TOOLS"), "/pkg/include")
	strGoYolintTool      = internV(resourcePatternRef("YOLINT"), "/yolint")
	strGoToolScript      = internV("$(S)/", "build/scripts/go_tool.py")
	strGoVcsInfoScript   = internV("$(S)/", "build/scripts/vcs_info.py")
	strGoVcsJson         = internV("$(VCS)/", "vcs.json")
	strGoMigrationsCfg   = internV("-migration.config=", "$(S)/", "build/rules/go/migrations.yaml")
	strGoScopelintCfg    = internV("-scopelint.config=", "$(S)/", "build/rules/go/extended_lint.yaml")
	strGoRiskyCfg        = internV("-riskyimports.config=", "$(S)/", "build/rules/go/risky_imports.yaml")
)

var (
	goToolCmdHeadLib = goToolCmdHeadChunk("lib")
	goToolCmdHeadExe = goToolCmdHeadChunk("exe")

	goToolCmdMid = []STR{
		internStr("++toolchain-root"), strGoToolsRoot,
		internStr("++host-os"), strLinux,
		internStr("++host-arch"), strAmd64,
		internStr("++targ-os"), strLinux,
		internStr("++targ-arch"), strAmd64,
		internStr("++output"),
	}

	goToolCmdVet = []STR{
		internStr("++vet"), strGoYolintTool,
		internStr("++vet-flags"),
		strGoMigrationsCfg,
		strGoScopelintCfg,
		strGoRiskyCfg,
		internStr("++debug-root-map"), internStr("source=/-S;build=/-B;tools=/-T"),
		internStr("++tools-root"), internStr("$(TOOL_ROOT)"),
		internStr("++srcs"),
	}

	goToolCmdFlags    = goToolCmdFlagsChunk(false)
	goToolCmdFlagsPic = goToolCmdFlagsChunk(true)

	goToolCmdPeers = []STR{internStr("++peers")}
	goToolCmdEnd   = []STR{internStr("--ya-end-command-file")}

	goExeCmdLinkFlags = []STR{
		strLinkFlags,
		strLinkmodeExternal,
		strCgoSrcs,
		internStr("++ld_plugins"),
		internStr("++vcs"),
	}

	goExeCmdExtld = []STR{internStr("++extld"), strClang, internStr("++extldflags")}

	goExtldWholeArchive = []STR{
		internStr("-Wl,--whole-archive"),
		internStr("-Wl,--no-whole-archive"),
		internStr("--cgo-peers"),
	}

	goExtldLinkerTail = []STR{
		internStr("-Wl,--no-rosegment"),
		internStr("-Wl,--build-id=sha1"),
		internStr("-lpthread"),
		internStr("-ldl"),
		internStr("-lresolv"),
	}

	goSymabisHead = []STR{
		strGoAsmTool,
		internStr("-trimpath"),
		internStr("$(SOURCE_ROOT)=>/-S;$(BUILD_ROOT)=>/-B;$(TOOL_ROOT)=>/-T"),
	}

	goSymabisDefs = []STR{
		strI, strGoToolsPkgInclude,
		internStr("-D"), internStr("GOOS_linux"),
		internStr("-D"), internStr("GOARCH_amd64"),
		internStr("-p"),
	}
)

func goToolCmdHeadChunk(mode string) []STR {
	return []STR{
		wrapccPython3STR,
		strGoToolScript,
		strYaStartCommandFile,
		strMode, internStr(mode),
		strStdLibPrefix, internStr(goStdPrefix + "/"),
		strArcProjectPrefix, internStr(goArcPrefix),
		strGoversion, internStr(goVersion),
		strLang2,
		strSourceRoot, strS,
		strBuildRoot, strB,
		strOutputRoot,
	}
}

func goToolCmdFlagsChunk(pic bool) []STR {
	out := []STR{strAsmFlags}

	if pic {
		out = append(out, strShared)
	}

	out = append(out, strCompileFlags)

	if pic {
		out = append(out, strShared)
	}

	return out
}

func isGoModuleType(name TOK) bool {
	return name == tokGoLibrary || name == tokGoProgram
}

func goImportPathFor(dir string) string {
	if rest, ok := strings.CutPrefix(dir, goStdPrefix+"/"); ok {
		return rest
	}

	if rest, ok := strings.CutPrefix(dir, goVendorPrefix+"/"); ok {
		return rest
	}

	return goArcPrefix + dir
}

func goImportDir(ctx *GenCtx, importerDir, imp string) string {
	if imp == "unsafe" || imp == "C" {
		return ""
	}

	if rest, ok := strings.CutPrefix(imp, goArcPrefix); ok {
		imp = rest
	}

	candidates := []string{goVendorPrefix + "/" + imp, goStdPrefix + "/" + imp, imp}

	if strings.HasPrefix(importerDir, goStdPrefix+"/") {
		candidates = []string{goStdPrefix + "/vendor/" + imp, goStdPrefix + "/" + imp, imp}
	}

	for _, cand := range candidates {
		if ctx.fs.isFile(source(cand), "ya.make") {
			return cand
		}
	}

	return ""
}

func applyGoImplicitPeerdirs(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	dir := instance.Path.rel()

	addPeer := func(p string) {
		if p == "" || p == dir {
			return
		}

		for _, have := range d.peerdirs {
			if have.string() == p {
				return
			}
		}

		d.peerdirs = append(d.peerdirs, internStr(p))
	}

	if d.moduleStmt.Name == tokGoProgram {
		addPeer(goStdPrefix + "/runtime/cgo")
		addPeer(goStdPrefix + "/runtime")
		addPeer("library/go/core/buildinfo")
	}

	if len(d.cgoSrcs) > 0 {
		addPeer("build/internal/platform/clang_toolchain_info")
		addPeer("build/platform/lld")
		addPeer(goStdPrefix + "/runtime/cgo")
	}

	for _, src := range d.srcs {
		rel := src.string()

		if !strings.HasSuffix(rel, ".go") {
			continue
		}

		data := ctx.fs.read(dir + "/" + rel)

		for _, imp := range parseGoImports(data) {
			addPeer(goImportDir(ctx, dir, imp))
		}
	}

	for _, src := range d.cgoSrcs {
		data := ctx.fs.read(dir + "/" + src.string())

		for _, imp := range parseGoImports(data) {
			addPeer(goImportDir(ctx, dir, imp))
		}
	}

	addPeer(goToolsPeer)
	addPeer(goYolintPeer)

	goIncl := make([]VFS, 0, 3)

	goIncl = append(goIncl, goStdRuntimeVFS)

	hasDotS := false

	for _, src := range d.srcs {
		if strings.HasSuffix(src.string(), ".s") {
			hasDotS = true

			break
		}
	}

	if hasDotS {
		goIncl = append(goIncl, goFakeIncludeVFS)
	}

	if goModuleUsesCgoC(d) {
		goIncl = append(goIncl, instance.Path)
	}

	d.addIncl = append(goIncl, d.addIncl...)

	for _, src := range goModuleCgoSFiles(d) {
		if d.perSrcCFlags == nil {
			d.perSrcCFlags = map[STR][]ARG{}
		}

		d.perSrcCFlags[src] = goCgoCFlags(d)
	}
}

type GoSrcsResult struct {
	GoFiles     []VFS
	AsmFiles    []VFS
	AsmInclSrcs []VFS
	SymabisRef  NodeRef
	SymabisOut  VFS
}

func (e *EmitContext) collectGoSource(meta SrcMeta, asm bool) {
	if e.goRes == nil {
		e.goRes = &GoSrcsResult{}
	}

	src := e.moduleSourceVFS(meta.Source)

	if asm {
		e.goRes.AsmFiles = append(e.goRes.AsmFiles, src)
	} else {
		e.goRes.GoFiles = append(e.goRes.GoFiles, src)
	}
}

func (e *EmitContext) goInclSplitArgs() []STR {
	if e.goInclSplit != nil {
		return e.goInclSplit
	}

	na := e.ctx.na
	joined := e.goCgoIncludeArgs()
	block := na.strs.alloc(2 * len(joined))
	k := 0
	dashI := strI

	for _, a := range joined {
		block[k] = dashI
		block[k+1] = internStr(strings.TrimPrefix(a.string(), "-I"))
		k += 2
	}

	na.strs.commit(k)
	e.goInclSplit = block[:k:k]

	return e.goInclSplit
}

func (e *EmitContext) flushGoSrcs() {
	if e.goRes == nil || len(e.goRes.AsmFiles) == 0 {
		return
	}

	ctx, instance := e.ctx, e.instance
	na := ctx.na
	dir := instance.Path.rel()
	out := build(dir, "/gen.symabis")

	tail := na.strs.alloc(5 + len(e.goRes.AsmFiles))
	nt := 0
	push := func(x STR) { tail[nt] = x; nt++ }

	push(internStr(goImportPathFor(dir)))

	if instance.Platform.PIC {
		push(strShared)
	}

	push(strGensymabis)
	push(argDashO.str())
	push(out.str())

	for _, src := range e.goRes.AsmFiles {
		push(src.str())
	}

	na.strs.commit(nt)

	e.goRes.AsmInclSrcs = e.goAsmIncludeSrcs()

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(goSymabisHead, e.goInclSplitArgs(), goSymabisDefs, tail[:nt:nt]), Env: goVcsEnv}),
		Env:          goVcsEnv,
		Inputs:       na.inputList(e.goRes.AsmFiles, e.goRes.AsmInclSrcs),
		KV:           &goToolKV,
		Outputs:      na.vfsList(out),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    goToolResources(na, e.peers.ResourceGlobals),
	}

	e.goRes.SymabisRef = ctx.emit.emitNode(node)
	e.goRes.SymabisOut = out
}

func (e *EmitContext) goAsmIncludeSrcs() []VFS {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	cfg := newScanContext(ctx.parsers, goAsmIncludeDirs, nil, includeScannerBasePaths(), instance.Path.rel())

	deduper.reset()

	bound := 0

	for _, src := range e.goRes.AsmFiles {
		deduper.add(src.strID())

		bound += walkClosure(e.scanner, src, cfg).len()
	}

	block := na.vfs.alloc(bound)
	k := 0

	for _, src := range e.goRes.AsmFiles {
		cv := walkClosure(e.scanner, src, cfg)

		cv.each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				block[k] = p
				k++
			}
		})
	}

	na.vfs.commit(k)

	return block[:k:k]
}

func goToolResources(na *NodeArenas, decls []ResourceDecl) []STR {
	block := na.strs.alloc(len(decls))

	for i, d := range decls {
		block[i] = d.Name
	}

	na.strs.commit(len(decls))

	return block[:len(decls):len(decls)]
}

func goToolPathEnv(tc ModuleToolchain) STR {
	clangBin := strings.TrimSuffix(tc.CC.string(), "/clang")

	return internV(clangBin, ":", "$(B)/resources/", resourcePatternOSSDKRoot, "/usr/bin")
}

func goCmdEnv(ctx *GenCtx, p *Platform, tc ModuleToolchain) EnvVars {
	path := goToolPathEnv(tc)
	key := [2]STR{path, p.MultiarchLibPathSTR}

	if env, ok := ctx.goEnvMemo[key]; ok {
		return env
	}

	env := EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS},
		{Name: envCC, Value: strClang},
		{Name: envCPATH, Value: strEmpty},
		{Name: envDYLD_LIBRARY_PATH, Value: p.MultiarchLibPathSTR},
		{Name: envGOARCH, Value: strAmd64},
		{Name: envGOOS, Value: strLinux},
		{Name: envLIBRARY_PATH, Value: strEmpty},
		{Name: cudaPathEnv, Value: path},
		{Name: envSDKROOT, Value: strEmpty},
	}

	if ctx.goEnvMemo == nil {
		ctx.goEnvMemo = map[[2]STR]EnvVars{}
	}

	ctx.goEnvMemo[key] = env

	return env
}

func goClassifyClosure(na *NodeArenas, resolved []resolvedPeer, closurePaths []VFS) (localARs, nonLocalARs, cgoARs []VFS) {
	deduper.reset()

	for _, rp := range resolved {
		if rp.result.ARPath != nil && isGoModuleType(rp.result.ModuleStmtName) {
			deduper.add(rp.result.ARPath.strID())
		}
	}

	classify := func(p VFS) int {
		switch {
		case deduper.has(p.strID()):
			return 0
		case isGoArchivePath(p.rel()):
			return 1
		default:
			return 2
		}
	}

	var counts [3]int

	for _, p := range closurePaths {
		counts[classify(p)]++
	}

	block := na.vfs.alloc(len(closurePaths))
	starts := [3]int{0, counts[0], counts[0] + counts[1]}
	fill := starts

	for _, p := range closurePaths {
		c := classify(p)

		block[fill[c]] = p
		fill[c]++
	}

	na.vfs.commit(len(closurePaths))

	return block[:fill[0]:fill[0]], block[starts[1]:fill[1]:fill[1]], block[starts[2]:fill[2]:fill[2]]
}

func goExtldflagsArgs(na *NodeArenas, p *Platform, tc ModuleToolchain, useArcadiaLibm bool) []STR {
	gdb := p.linkerSelectionGDBIndexFlags()
	bound := 1 + len(p.SysrootArgs) + len(goExtldWholeArchive) + 1 + len(p.LinkPreludeExtra) + 1 + 2 + len(gdb) + 2 + len(goExtldLinkerTail) + len(p.SystemLibs) + 1
	block := na.strs.alloc(bound)
	k := 0
	push := func(x STR) { block[k] = x; k++ }

	push(p.TargetArg)

	for _, x := range p.SysrootArgs {
		push(x)
	}

	for _, x := range goExtldWholeArchive {
		push(x)
	}

	if p.CompressDebugSections {
		push(argWlCompressDebugSectionsZstd.str())
	}

	for _, x := range p.LinkPreludeExtra {
		push(x)
	}

	push(argWlNoAsNeeded.str())

	if p.PIC {
		push(argFPIC.str())
	}

	for _, f := range gdb {
		push(internStr(f))
	}

	if p.PIC {
		push(argFPIC.str())
	}

	push(argFuseLdLld.str())
	push(internV("--ld-path=", tc.LLD.string()))

	for _, x := range goExtldLinkerTail {
		push(x)
	}

	for _, x := range p.SystemLibs {
		push(x)
	}

	if !useArcadiaLibm {
		push(argDashLm.str())
	}

	na.strs.commit(k)

	return block[:k:k]
}

func (e *EmitContext) goToolchainSboms(withLinker bool) ([]NodeRef, []VFS) {
	ctx, instance := e.ctx, e.instance

	if !sbomActive(ctx, instance) {
		return nil, nil
	}

	var refs []NodeRef
	var paths []VFS

	add := func(r *NodeRef, p *VFS) {
		if r != nil && p != nil {
			refs = append(refs, *r)
			paths = append(paths, *p)
		}
	}

	if withLinker {
		add(clangToolchainSbomComponent(ctx, instance.Platform))
	}

	add(pythonToolchainSbomComponent(ctx, instance.Platform))

	return refs, paths
}

func goPeerSrcClosure(ctx *GenCtx, resolved []resolvedPeer, own []VFS, extra []VFS) []VFS {
	deduper.reset()

	total := len(own) + len(extra) + 1

	for _, rp := range resolved {
		total += len(rp.result.GoSrcClosure)
	}

	block := ctx.vfsSlices.alloc(total)
	k := 0

	add := func(p VFS) {
		if deduper.add(p.strID()) {
			block[k] = p
			k++
		}
	}

	for _, rp := range resolved {
		for _, p := range rp.result.GoSrcClosure {
			add(p)
		}
	}

	for _, p := range own {
		if p.isSource() {
			add(p)
		}
	}

	for _, p := range extra {
		add(p)
	}

	return ctx.vfsSlices.intern(block[:k])
}

func (e *EmitContext) goToolCmdFlagsFor() []STR {
	if e.instance.Platform.PIC {
		return goToolCmdFlagsPic
	}

	return goToolCmdFlags
}

func (e *EmitContext) emitGoPackage(resolved []resolvedPeer, objRefs []NodeRef, objOuts []VFS, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, peerSbomRefs []NodeRef, peerSbomPaths []VFS, ownSbomRef *NodeRef, ownSbomPath *VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS, []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.rel()
	outName := dir[strings.LastIndexByte(dir, '/')+1:]
	outPath := build(dir, "/", outName, ".a")
	goRes := e.goRes

	if goRes == nil {
		goRes = &GoSrcsResult{}
	}

	srcCap := 1 + len(objOuts) + len(goRes.GoFiles) + len(goRes.AsmFiles) + len(d.cgoSrcs)
	srcArgs := na.strs.alloc(srcCap)
	srcInputs := na.vfs.alloc(srcCap)
	nsrc := 0

	addSrc := func(p VFS) {
		srcArgs[nsrc] = p.str()
		srcInputs[nsrc] = p
		nsrc++
	}

	if goRes.SymabisRef != 0 {
		addSrc(goRes.SymabisOut)
	}

	if len(d.cgoSrcs) > 0 {
		objSuf := instance.Platform.objectSuffix()
		isCgoObj := func(p VFS) bool {
			rel := p.rel()

			return strings.HasSuffix(rel, ".cgo2.c"+objSuf) || strings.HasSuffix(rel, "/_cgo_export.c"+objSuf)
		}
		isDotSObj := func(p VFS) bool {
			return strings.HasSuffix(p.rel(), ".S.o")
		}

		for _, o := range objOuts {
			if !isCgoObj(o) && !isDotSObj(o) {
				addSrc(o)
			}
		}

		for _, o := range objOuts {
			if isDotSObj(o) {
				addSrc(o)
			}
		}

		importGoRel := dir + "/_cgo_import.go"

		for _, src := range goRes.GoFiles {
			if src.isBuild() && src.rel() != importGoRel {
				addSrc(src)
			}
		}

		for _, o := range objOuts {
			if isCgoObj(o) {
				addSrc(o)
			}
		}

		for _, src := range goRes.GoFiles {
			if src.isBuild() && src.rel() == importGoRel {
				addSrc(src)
			}
		}

		for _, src := range goRes.GoFiles {
			if src.isSource() {
				addSrc(src)
			}
		}
	} else {
		for _, o := range objOuts {
			addSrc(o)
		}

		for _, src := range goRes.GoFiles {
			addSrc(src)
		}
	}

	for _, src := range goRes.AsmFiles {
		addSrc(src)
	}

	for _, f := range d.cgoSrcs {
		cgoSrc := resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)

		srcArgs[nsrc] = cgoSrc.str()
		srcInputs[nsrc] = cgoSrc
		nsrc++
	}

	na.strs.commit(nsrc)
	na.vfs.commit(nsrc)

	plain := nsrc - len(d.cgoSrcs)
	cgoSrcArgs := srcArgs[plain:nsrc:nsrc]

	srcArgs = srcArgs[:plain:plain]

	ownInputs := srcInputs[:nsrc:nsrc]
	localARs, _, _ := goClassifyClosure(na, resolved, peerArchivePaths)
	tail := na.strs.alloc(4 + len(cgoSrcArgs) + len(localARs))
	nt := 0
	pushTail := func(x STR) { tail[nt] = x; nt++ }

	pushTail(strLinkFlags)
	pushTail(strLinkmodeExternal)
	pushTail(strCgoSrcs)

	for _, x := range cgoSrcArgs {
		pushTail(x)
	}

	pushTail(goToolCmdPeers[0])

	for _, p := range localARs {
		pushTail(internStr(p.rel()))
	}

	na.strs.commit(nt)

	srcClosureExtras := goRes.AsmInclSrcs

	if len(d.cgoSrcs) > 0 {
		cgoAux := append(goModuleCgoCFiles(d), goModuleCgoSFiles(d)...)
		copyScripts := ctx.scripts[copyFsToolsVFS]
		linkOScripts := ctx.scripts[linkOScriptVFS]
		extrasCap := len(srcClosureExtras) + 2 + len(copyScripts) + len(linkOScripts)
		auxSrcs := make([]VFS, len(cgoAux))

		for i, f := range cgoAux {
			auxSrcs[i] = resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)
			extrasCap += 1 + walkClosure(e.scanner, auxSrcs[i], d.cc.ScanCfg).len()
		}

		block := na.vfs.alloc(extrasCap)
		k := 0
		push := func(p VFS) { block[k] = p; k++ }

		for _, p := range srcClosureExtras {
			push(p)
		}

		push(cgo1WrapperVFS)
		push(wrapccPyVFS)

		for _, p := range copyScripts {
			push(p)
		}

		for _, p := range linkOScripts {
			push(p)
		}

		for _, src := range auxSrcs {
			push(src)

			cv := walkClosure(e.scanner, src, d.cc.ScanCfg)

			cv.each(func(p VFS) {
				if p.isSource() {
					push(p)
				}
			})
		}

		na.vfs.commit(k)

		srcClosureExtras = block[:k:k]
	}

	srcClosure := goPeerSrcClosure(ctx, resolved, ownInputs, srcClosureExtras)

	sbomRefs, sbomPaths := e.goToolchainSboms(false)

	deduper.reset()

	mergedSbomRefs := na.noderefs.alloc(len(peerSbomRefs) + len(sbomRefs) + 1)
	mergedSbomPaths := na.vfs.alloc(len(peerSbomPaths) + len(sbomPaths) + 1)
	merged := 0

	addSbom := func(ref NodeRef, p VFS) {
		if deduper.add(p.strID()) {
			mergedSbomRefs[merged] = ref
			mergedSbomPaths[merged] = p
			merged++
		}
	}

	for i, p := range peerSbomPaths {
		addSbom(peerSbomRefs[i], p)
	}

	for i, p := range sbomPaths {
		addSbom(sbomRefs[i], p)
	}

	if ownSbomPath != nil && ownSbomRef != nil {
		addSbom(*ownSbomRef, *ownSbomPath)
	}

	na.noderefs.commit(merged)
	na.vfs.commit(merged)

	hasGoSbom := ownSbomPath != nil

	for _, p := range mergedSbomPaths[:merged] {
		if strings.HasSuffix(p.rel(), ".GO.component.sbom") {
			hasGoSbom = true

			break
		}
	}

	deduper.reset()

	for _, p := range ownInputs {
		deduper.add(p.strID())
	}

	for _, p := range goToolScriptInputsChunk {
		deduper.add(p.strID())
	}

	for _, p := range peerArchivePaths {
		deduper.add(p.strID())
	}

	extraBlock := na.vfs.alloc(len(srcClosure) + 1 + merged + 1)
	nx := 0

	for _, p := range srcClosure {
		if deduper.add(p.strID()) {
			extraBlock[nx] = p
			nx++
		}
	}

	if len(d.cgoSrcs) > 0 {
		extraBlock[nx] = build(dir, "/_cgo_main.c", instance.Platform.objectSuffix())
		nx++
	}

	for _, p := range mergedSbomPaths[:merged] {
		extraBlock[nx] = p
		nx++
	}

	if hasGoSbom {
		extraBlock[nx] = source(sbomGenScriptRel)
		nx++
	}

	na.vfs.commit(nx)

	extraInputs := extraBlock[:nx:nx]
	inputs := na.inputList(ownInputs, goToolScriptInputsChunk, peerArchivePaths, extraInputs)

	depBlock := na.noderefs.alloc(1 + len(objRefs) + len(peerArchiveRefs) + merged)
	ndep := 0

	if goRes.SymabisRef != 0 {
		depBlock[ndep] = goRes.SymabisRef
		ndep++
	}

	ndep += copy(depBlock[ndep:], objRefs)
	ndep += copy(depBlock[ndep:], peerArchiveRefs)
	ndep += copy(depBlock[ndep:], mergedSbomRefs[:merged])

	na.noderefs.commit(ndep)

	deps := e.resolveCodegenDepRefsChunks(inputs, depBlock[:ndep])

	env := goCmdEnv(ctx, instance.Platform, d.tc)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(
			goToolCmdHeadLib,
			na.strList(build(dir).str()),
			goToolCmdMid,
			na.strList(outPath.str()),
			goToolCmdVet,
			srcArgs,
			e.goToolCmdFlagsFor(),
			tail[:nt:nt],
			goToolCmdEnd,
		), Env: env}),
		Env:          env,
		Inputs:       inputs,
		KV:           &goKV,
		Outputs:      na.vfsList(outPath, build(dir, "/", outName, ".a.vet.out"), build(dir, "/", outName, ".a.vet.txt")),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps,
		Resources:    goToolResources(na, resourceGlobals),
	}

	return ctx.emit.emitNode(node), outPath, srcClosure
}

func (e *EmitContext) emitGoExe(resolved []resolvedPeer, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, peerSbomRefs []NodeRef, peerSbomPaths []VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.rel()
	outName := programBinaryName(instance, d.moduleStmt)
	outPath := build(dir, "/", outName)
	goRes := e.goRes

	if goRes == nil {
		goRes = &GoSrcsResult{}
	}

	vcsCPath := build(dir, "/__vcs_version__.c")
	vcsOPath := build(dir, "/__vcs_version__.c", instance.Platform.objectSuffix())
	vcsGoPath := build(dir, "/__vcs_version__.go")

	cmd0 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsInfo(d.tc, vcsCPath.string())), Env: goVcsEnv}
	cmd1 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsCompileForced(instance.Platform, d.tc, vcsCPath.string(), vcsOPath.string(), d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize, true)), Env: instance.Platform.ToolEnvVars}
	cmd2 := Cmd{CmdArgs: na.chunkList(na.strList(
		wrapccPython3STR,
		strGoVcsInfoScript,
		strOutputGo,
		strGoVcsJson,
		vcsGoPath.str(),
		internStr(goArcPrefix),
	)), Env: goVcsEnv}

	srcArgs := na.strs.alloc(len(goRes.GoFiles))

	for i, src := range goRes.GoFiles {
		srcArgs[i] = src.str()
	}

	na.strs.commit(len(goRes.GoFiles))

	srcArgs = srcArgs[:len(goRes.GoFiles):len(goRes.GoFiles)]
	localARs, nonLocalARs, cgoARs := goClassifyClosure(na, resolved, peerArchivePaths)
	tail := na.strs.alloc(4 + len(localARs) + len(nonLocalARs) + len(cgoARs))
	nt := 0
	pushTail := func(x STR) { tail[nt] = x; nt++ }

	pushTail(goToolCmdPeers[0])

	for _, p := range localARs {
		pushTail(internStr(p.rel()))
	}

	pushTail(strNonLocalPeers)

	for _, p := range nonLocalARs {
		pushTail(internStr(p.rel()))
	}

	pushTail(strCgoPeers)
	pushTail(internStr(vcsOPath.rel()))

	for _, p := range cgoARs {
		pushTail(internStr(p.rel()))
	}

	na.strs.commit(nt)

	sbomEmbed := instance.Platform.BuildRelease && sbomActive(ctx, instance) && len(peerSbomPaths) > 0
	extraCap := 2

	if sbomEmbed {
		extraCap += len(peerSbomPaths) + 1
	}

	extraBlock := na.vfs.alloc(extraCap)

	extraBlock[0] = ldSvnInterfaceVFS
	extraBlock[1] = ldSvnversionHVFS

	nx := 2

	if sbomEmbed {
		nx += copy(extraBlock[nx:], peerSbomPaths)
		extraBlock[nx] = linkSbomScriptVFS
		nx++
	}

	na.vfs.commit(nx)

	extraInputs := extraBlock[:nx:nx]

	inputs := na.inputList(
		goRes.GoFiles,
		goToolScriptInputsChunk,
		ctx.scripts[ldVcsInfoVFS],
		ctx.scripts[ldFsToolsVFS],
		peerArchivePaths,
		extraInputs,
	)

	deps := na.noderefs.alloc(len(peerArchiveRefs) + len(peerSbomRefs) + 1)
	k := copy(deps, peerArchiveRefs)

	if instance.Platform.BuildRelease && sbomActive(ctx, instance) {
		k += copy(deps[k:], peerSbomRefs)
	}

	deps[k] = ctx.vcsRef
	k++

	na.noderefs.commit(k)

	env := goCmdEnv(ctx, instance.Platform, d.tc)
	goCmd := Cmd{CmdArgs: na.chunkList(
		goToolCmdHeadExe,
		na.strList(build(dir).str()),
		goToolCmdMid,
		na.strList(outPath.str()),
		goToolCmdVet,
		srcArgs,
		e.goToolCmdFlagsFor(),
		goExeCmdLinkFlags,
		na.strList(vcsGoPath.str()),
		goExeCmdExtld,
		goExtldflagsArgs(na, instance.Platform, d.tc, d.useArcadiaLibm),
		tail[:nt:nt],
		goToolCmdEnd,
	), Env: env}

	sbomJSON := build(dir, "/__sbomdata.json").string()

	var linkSbomCmd, sbomObjcopyCmd Cmd

	if sbomEmbed {
		linkSbomCmd = Cmd{CmdArgs: na.chunkList(composeLDCmdLinkSbom(d.tc, strGo, dir, sbomJSON, peerSbomPaths)), Cwd: strB, Env: goVcsEnv}
		sbomObjcopyCmd = Cmd{CmdArgs: na.chunkList(composeLDCmdSbomObjcopy(d.tc, sbomJSON, outPath.string())), Env: goVcsEnv}
	}

	cmds := na.cmds.alloc(6)
	kc := 0
	pushCmd := func(c Cmd) { cmds[kc] = c; kc++ }

	pushCmd(cmd0)
	pushCmd(cmd1)
	pushCmd(cmd2)

	if sbomEmbed {
		pushCmd(linkSbomCmd)
	}

	pushCmd(goCmd)

	if sbomEmbed {
		pushCmd(sbomObjcopyCmd)
	}

	na.cmds.commit(kc)

	node := Node{
		Platform:     instance.Platform,
		Cmds:         cmds[:kc:kc],
		Env:          env,
		Inputs:       inputs,
		KV:           &goLdKV,
		Outputs:      na.vfsList(outPath, build(dir, "/", outName, ".vet.txt")),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      deps[:k:k],
		Resources:    goToolResources(na, resourceGlobals),
	}

	return ctx.emit.emitNode(node), outPath
}

func isGoArchivePath(rel string) bool {
	if !strings.HasSuffix(rel, ".a") {
		return false
	}

	slash := strings.LastIndexByte(rel, '/')

	if slash < 0 {
		return false
	}

	base := strings.TrimSuffix(rel[slash+1:], ".a")
	dir := rel[:slash]

	return base == dir[strings.LastIndexByte(dir, '/')+1:]
}
