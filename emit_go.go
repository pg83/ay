package main

import (
	"strings"
)

var (
	goKV                 = KV{P: pkGO, PC: pcLightRed, ShowOut: true}
	goToolKV             = KV{P: pkGoTool, PC: pcLightBlue, ShowOut: true}
	goLdKV               = KV{P: pkLD, PC: pcLightRed, ShowOut: true}
	goVcsEnv             = envVarsVCS
	goStdRuntimeVFS      = source(goStdPrefix + "/runtime")
	strGoToolsRoot       = internV("$(B)/resources/", "GO_TOOLS")
	strGoAsmTool         = internV("$(B)/resources/", "GO_TOOLS", "/pkg/tool/linux_amd64/asm")
	strGoCgoTool         = internV("$(B)/resources/", "GO_TOOLS", "/pkg/tool/linux_amd64/cgo")
	strGoToolsPkgInclude = internV("$(B)/resources/", "GO_TOOLS", "/pkg/include")
	strGoYolintTool      = internV("$(B)/resources/", "YOLINT", "/yolint")
	strGoToolScript      = internV("$(S)/", "build/scripts/go_tool.py")
	strGoVcsInfoScript   = internV("$(S)/", "build/scripts/vcs_info.py")
	strGoVcsJson         = bldVcsJson.any()
	strGoMigrationsCfg   = internV("-migration.config=", "$(S)/", "build/rules/go/migrations.yaml")
	strGoScopelintCfg    = internV("-scopelint.config=", "$(S)/", "build/rules/go/extended_lint.yaml")
	strGoRiskyCfg        = internV("-riskyimports.config=", "$(S)/", "build/rules/go/risky_imports.yaml")
	goToolCmdHeadLib     = goToolCmdHeadChunk("lib")
	goToolCmdHeadExe     = goToolCmdHeadChunk("exe")
	goToolCmdFlags       = goToolCmdFlagsChunk(false)
	goToolCmdFlagsPic    = goToolCmdFlagsChunk(true)
	goToolCmdPeers       = []STR{internStr("++peers")}
	goToolCmdEnd         = []ANY{internStr("--ya-end-command-file").any()}
	goExeCmdExtld        = []ANY{internStr("++extld").any(), strClang.any(), internStr("++extldflags").any()}
)

var goAsmIncludeDirs = []VFS{
	goStdRuntimeVFS,
	goFakeIncludeVFS,
	contribLibsLinuxHeaders,
	contribLibsLinuxHeadersNf,
}

var goToolScriptInputsChunk = []VFS{
	source("build/scripts/go_tool.py"),
	buildScriptsProcessCommandFilesPy,
	source("build/scripts/process_whole_archive_option.py"),
	source("build/rules/go/migrations.yaml"),
	source("build/rules/go/extended_lint.yaml"),
	source("build/rules/go/risky_imports.yaml"),
}

var goToolCmdMid = []ANY{
	internStr("++toolchain-root").any(), strGoToolsRoot.any(),
	internStr("++host-os").any(), strLinux.any(),
	internStr("++host-arch").any(), strAmd64.any(),
	internStr("++targ-os").any(), strLinux.any(),
	internStr("++targ-arch").any(), strAmd64.any(),
	internStr("++output").any(),
}

var goToolCmdVet = []ANY{
	internStr("++vet").any(), strGoYolintTool.any(),
	internStr("++vet-flags").any(),
	strGoMigrationsCfg.any(),
	strGoScopelintCfg.any(),
	strGoRiskyCfg.any(),
	internStr("++debug-root-map").any(), internStr("source=/-S;build=/-B;tools=/-T").any(),
	internStr("++tools-root").any(), internStr("$(TOOL_ROOT)").any(),
	internStr("++srcs").any(),
}

var goExeCmdLinkFlags = []ANY{
	strLinkFlags.any(),
	strLinkmodeExternal.any(),
	strCgoSrcs.any(),
	internStr("++ld_plugins").any(),
	internStr("++vcs").any(),
}

var goExtldWholeArchive = []ANY{
	internStr("-Wl,--whole-archive").any(),
	internStr("-Wl,--no-whole-archive").any(),
	internStr("--cgo-peers").any(),
}

var goExtldLinkerTail = []ANY{
	internStr("-Wl,--no-rosegment").any(),
	internStr("-Wl,--build-id=sha1").any(),
	internStr("-lpthread").any(),
	internStr("-ldl").any(),
	internStr("-lresolv").any(),
}

var goSymabisHead = []ANY{
	strGoAsmTool.any(),
	internStr("-trimpath").any(),
	internStr("$(S)=>/-S;$(B)=>/-B;$(TOOL_ROOT)=>/-T").any(),
}

var goSymabisDefs = []ANY{
	strI.any(), strGoToolsPkgInclude.any(),
	internStr("-D").any(), internStr("GOOS_linux").any(),
	internStr("-D").any(), internStr("GOARCH_amd64").any(),
	internStr("-p").any(),
}

const (
	goStdPrefix    = "contrib/go/_std_1.26/src"
	goArcPrefix    = "a.yandex-team.ru/"
	goVersion      = "1.26"
	goToolsPeer    = "build/external_resources/go_tools"
	goYolintPeer   = "build/external_resources/yolint"
	goVendorPrefix = "vendor"
)

func goToolCmdHeadChunk(mode string) []ANY {
	return []ANY{
		wrapccPython3STR.any(),
		strGoToolScript.any(),
		strYaStartCommandFile.any(),
		strMode.any(), internStr(mode).any(),
		strStdLibPrefix.any(), internStr(goStdPrefix + "/").any(),
		strArcProjectPrefix.any(), internStr(goArcPrefix).any(),
		strGoversion.any(), internStr(goVersion).any(),
		strLang2.any(),
		strSourceRoot.any(), strS.any(),
		strBuildRoot.any(), strB.any(),
		strOutputRoot.any(),
	}
}

func goToolCmdFlagsChunk(pic bool) []ANY {
	out := []ANY{strAsmFlags.any()}

	if pic {
		out = append(out, strShared.any())
	}

	out = append(out, strCompileFlags.any())

	if pic {
		out = append(out, strShared.any())
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
		if ctx.fs.isFile(internStr(cand), "ya.make") {
			return cand
		}
	}

	return ""
}

func applyGoImplicitPeerdirs(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	dir := instance.Path.relString()

	addPeer := func(p string) {
		if p == "" || p == dir {
			return
		}

		for _, have := range d.peerdirs {
			if have.string() == p {
				return
			}
		}

		d.peerdirs = append(d.peerdirs, internStr(p).any())
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

	for _, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		src := meta.Source
		rel := src.relOrSelf().string()

		if !strings.HasSuffix(rel, ".go") {
			continue
		}

		path, ok := goReadableSourcePath(dir, src)

		if !ok {
			continue
		}

		data := ctx.fs.read(path)

		for _, imp := range parseGoImports(data) {
			addPeer(goImportDir(ctx, dir, imp.string()))
		}
	}

	for _, src := range d.cgoSrcs {
		path, ok := goReadableSourcePath(dir, src)

		if !ok {
			continue
		}

		data := ctx.fs.read(path)

		for _, imp := range parseGoImports(data) {
			addPeer(goImportDir(ctx, dir, imp.string()))
		}
	}

	addPeer(goToolsPeer)
	addPeer(goYolintPeer)

	goIncl := make([]VFS, 0, 3)

	goIncl = append(goIncl, goStdRuntimeVFS)

	hasDotS := false

	for _, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		src := meta.Source

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

	cgoFlags := goCgoCFlags(d)

	for i := range d.srcs {
		meta := &d.srcs[i]

		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		rel := meta.Source.string()

		if strings.HasSuffix(rel, ".S") || strings.HasSuffix(rel, ".c") || strings.HasSuffix(rel, ".cxx") {
			meta.Compile.CFlags = concat(meta.Compile.CFlags, cgoFlags)
		}
	}
}

func goReadableSourcePath(module string, src ANY) (string, bool) {
	if v := src.vfs(); v != 0 {
		if v.isBuild() {
			return "", false
		}

		return v.relString(), true
	}

	return module + "/" + src.string(), true
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

func (e *EmitContext) goInclSplitArgs() []ANY {
	if e.goInclSplit != nil {
		return e.goInclSplit
	}

	na := e.ctx.na
	joined := e.goCgoIncludeArgs()
	block := na.anys.alloc(2 * len(joined))
	k := 0
	dashI := strI.any()

	for _, a := range joined {
		block[k] = dashI
		block[k+1] = internStr(strings.TrimPrefix(a.string(), "-I")).any()
		k += 2
	}

	na.anys.commit(k)
	e.goInclSplit = block[:k:k]

	return e.goInclSplit
}

func (e *EmitContext) flushGoSrcs() {
	if e.goRes == nil || len(e.goRes.AsmFiles) == 0 {
		return
	}

	ctx, instance := e.ctx, e.instance
	na := ctx.na
	dir := instance.Path.relString()
	out := build(dir, "/gen.symabis")
	tail := na.anys.alloc(5 + len(e.goRes.AsmFiles))
	nt := 0
	push := func(x STR) { tail[nt] = x.any(); nt++ }

	push(internStr(goImportPathFor(dir)))

	if instance.Platform.PIC {
		push(strShared)
	}

	push(strGensymabis)
	push(argDashO.str())

	tail[nt] = out.any()
	nt++

	for _, src := range e.goRes.AsmFiles {
		tail[nt] = src.any()
		nt++
	}

	na.anys.commit(nt)

	e.goRes.AsmInclSrcs = e.goAsmIncludeSrcs()
	depRefs := resolveCodegenDepRefsIncl(ctx, instance, na, e.goRes.AsmFiles)

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(goSymabisHead, e.goInclSplitArgs(), goSymabisDefs, tail[:nt:nt]), Env: goVcsEnv}),
		Env:          goVcsEnv,
		Inputs:       na.inputList(na.vfsList(e.goRes.AsmFiles...), e.goRes.AsmInclSrcs),
		KV:           &goToolKV,
		Outputs:      na.vfsList(out),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    goToolResources(na, e.peers.ResourceGlobals),
		DepRefs:      depRefs,
	}

	e.goRes.SymabisRef = ctx.emit.emitNode(node)
	e.goRes.SymabisOut = out
}

func (e *EmitContext) goAsmIncludeSrcs() []VFS {
	ctx := e.ctx
	na := ctx.na
	var out []VFS
	bound := 0
	cvs := e.cvScratch[:0]

	for _, src := range e.goRes.AsmFiles {
		cv := e.scanner.walkClosure(src, e.d.scanCtx, scanDomainGoAsm)

		cvs = append(cvs, cv)
		bound += cv.len()
	}

	e.cvScratch = retainMaxLen(e.cvScratch, cvs)

	dedupers.with(func(deduper *DeDuper) {
		for _, src := range e.goRes.AsmFiles {
			deduper.add(src.strID())
		}

		block := na.vfs.alloc(bound)
		k := 0

		for _, cv := range cvs {
			cv.each(func(p VFS) {
				if p.isSource() && deduper.add(p.strID()) {
					block[k] = p
					k++
				}
			})
		}

		na.vfs.commit(k)
		out = block[:k:k]
	})

	return out
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
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()},
		{Name: envCC, Value: strClang.any()},
		{Name: envCPATH, Value: strEmpty.any()},
		{Name: envDYLD_LIBRARY_PATH, Value: p.MultiarchLibPathSTR.any()},
		{Name: envGOARCH, Value: strAmd64.any()},
		{Name: envGOOS, Value: strLinux.any()},
		{Name: envLIBRARY_PATH, Value: strEmpty.any()},
		{Name: cudaPathEnv, Value: path.any()},
		{Name: envSDKROOT, Value: strEmpty.any()},
	}

	if ctx.goEnvMemo == nil {
		ctx.goEnvMemo = map[[2]STR]EnvVars{}
	}

	ctx.goEnvMemo[key] = env

	if ownershipOn {
		registerOwnedSlice(env)
	}

	return env
}

func goClassifyClosure(na *NodeArenas, resolved []ResolvedPeer, closurePaths []VFS) (localARs, nonLocalARs, cgoARs []VFS) {
	dedupers.with(func(deduper *DeDuper) {
		for _, rp := range resolved {
			if rp.result.ARPath != nil && isGoModuleType(rp.result.ModuleStmtName) {
				deduper.add(rp.result.ARPath.strID())
			}
		}

		classify := func(p VFS) int {
			switch {
			case deduper.has(p.strID()):
				return 0
			case isGoArchivePath(p.relString()):
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

		localARs = block[:fill[0]:fill[0]]
		nonLocalARs = block[starts[1]:fill[1]:fill[1]]
		cgoARs = block[starts[2]:fill[2]:fill[2]]
	})

	return
}

func goExtldflagsArgs(na *NodeArenas, p *Platform, tc ModuleToolchain, useArcadiaLibm bool) []ANY {
	gdb := p.linkerSelectionGDBIndexFlags()
	bound := 1 + len(p.SysrootArgs) + len(goExtldWholeArchive) + 1 + len(p.LinkPreludeExtra) + 1 + 2 + len(gdb) + 2 + len(goExtldLinkerTail) + len(p.SystemLibs) + 1
	block := na.anys.alloc(bound)
	k := 0
	push := func(x ANY) { block[k] = x; k++ }

	push(p.TargetArg.any())

	for _, x := range p.SysrootArgs {
		push(x)
	}

	for _, x := range goExtldWholeArchive {
		push(x)
	}

	if p.CompressDebugSections {
		push(argWlCompressDebugSectionsZstd.any())
	}

	for _, x := range p.LinkPreludeExtra {
		push(x)
	}

	push(argWlNoAsNeeded.any())

	if p.PIC {
		push(argFPIC.any())
	}

	for _, f := range gdb {
		push(internStr(f).any())
	}

	if p.PIC {
		push(argFPIC.any())
	}

	push(argFuseLdLld.any())
	push(internV("--ld-path=", tc.LLD.prefix(), tc.LLD.relString()).any())

	for _, x := range goExtldLinkerTail {
		push(x)
	}

	for _, x := range p.SystemLibs {
		push(x)
	}

	if !useArcadiaLibm {
		push(argDashLm.any())
	}

	na.anys.commit(k)

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

func goPeerSrcClosure(ctx *GenCtx, resolved []ResolvedPeer, own []VFS, extra []VFS) []VFS {
	total := len(own) + len(extra) + 1

	for _, rp := range resolved {
		total += len(rp.result.GoSrcClosure)
	}

	var out []VFS

	dedupers.with(func(deduper *DeDuper) {
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

		out = ctx.vfsSlices.intern(block[:k])
	})

	return out
}

func (e *EmitContext) goToolCmdFlagsFor() []ANY {
	if e.instance.Platform.PIC {
		return goToolCmdFlagsPic
	}

	return goToolCmdFlags
}

func (e *EmitContext) emitGoPackage(resolved []ResolvedPeer, objRefs []NodeRef, objOuts []VFS, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, peerSbomRefs []NodeRef, peerSbomPaths []VFS, ownSbomRef *NodeRef, ownSbomPath *VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS, []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()
	outName := dir[strings.LastIndexByte(dir, '/')+1:]
	outPath := build(dir, "/", outName, ".a")
	goRes := e.goRes

	if goRes == nil {
		goRes = &GoSrcsResult{}
	}

	srcCap := 1 + len(objOuts) + len(goRes.GoFiles) + len(goRes.AsmFiles) + len(d.cgoSrcs)
	srcArgs := na.anys.alloc(srcCap)
	srcInputs := na.vfs.alloc(srcCap)
	nsrc := 0

	addSrc := func(p VFS) {
		srcArgs[nsrc] = p.any()
		srcInputs[nsrc] = p
		nsrc++
	}

	if goRes.SymabisRef != 0 {
		addSrc(goRes.SymabisOut)
	}

	if len(d.cgoSrcs) > 0 {
		objSuf := instance.Platform.objectSuffix()

		isCgoObj := func(p VFS) bool {
			rel := p.relString()

			return strings.HasSuffix(rel, ".cgo2.c"+objSuf) || strings.HasSuffix(rel, "/_cgo_export.c"+objSuf)
		}

		isDotSObj := func(p VFS) bool {
			return strings.HasSuffix(p.relString(), ".S.o")
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
			if src.isBuild() && src.relString() != importGoRel {
				addSrc(src)
			}
		}

		for _, o := range objOuts {
			if isCgoObj(o) {
				addSrc(o)
			}
		}

		for _, src := range goRes.GoFiles {
			if src.isBuild() && src.relString() == importGoRel {
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

		srcArgs[nsrc] = cgoSrc.any()
		srcInputs[nsrc] = cgoSrc
		nsrc++
	}

	na.anys.commit(nsrc)
	na.vfs.commit(nsrc)

	plain := nsrc - len(d.cgoSrcs)
	cgoSrcArgs := srcArgs[plain:nsrc:nsrc]

	srcArgs = srcArgs[:plain:plain]

	ownInputs := srcInputs[:nsrc:nsrc]
	localARs, _, _ := goClassifyClosure(na, resolved, peerArchivePaths)
	tail := na.anys.alloc(4 + len(cgoSrcArgs) + len(localARs))
	nt := 0
	pushTail := func(x STR) { tail[nt] = x.any(); nt++ }

	pushTail(strLinkFlags)
	pushTail(strLinkmodeExternal)
	pushTail(strCgoSrcs)

	for _, x := range cgoSrcArgs {
		tail[nt] = x
		nt++
	}

	pushTail(goToolCmdPeers[0])

	for _, p := range localARs {
		pushTail(p.rel())
	}

	na.anys.commit(nt)

	srcClosureExtras := goRes.AsmInclSrcs

	if len(d.cgoSrcs) > 0 {
		cgoAux := append(goModuleCgoCFiles(d), goModuleCgoSFiles(d)...)
		copyScripts := ctx.scripts[copyFsToolsVFS.rel()]
		linkOScripts := ctx.scripts[linkOScriptVFS.rel()]
		extrasCap := len(srcClosureExtras) + 2 + len(copyScripts) + len(linkOScripts)
		auxSrcs := make([]VFS, len(cgoAux))

		for i, f := range cgoAux {
			auxSrcs[i] = resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)
			extrasCap += 1 + e.scanner.walkClosure(auxSrcs[i], d.scanCtx, scanDomainCC).len()
		}

		cvs := e.cvScratch[:0]

		for _, src := range auxSrcs {
			cvs = append(cvs, e.scanner.walkClosure(src, d.scanCtx, scanDomainCC))
		}

		e.cvScratch = retainMaxLen(e.cvScratch, cvs)

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

		for i, src := range auxSrcs {
			push(src)

			cvs[i].each(func(p VFS) {
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
	var mergedSbomRefs []NodeRef
	var merged int
	var extraInputs []VFS

	dedupers.with(func(deduper *DeDuper) {
		mergedSbomRefs = na.noderefs.alloc(len(peerSbomRefs) + len(sbomRefs) + 1)
		mergedSbomPaths := na.vfs.alloc(len(peerSbomPaths) + len(sbomPaths) + 1)

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
			if strings.HasSuffix(p.relString(), ".GO.component.sbom") {
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

		extraInputs = extraBlock[:nx:nx]
	})
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
			na.anyList(build(dir).any()),
			goToolCmdMid,
			na.anyList(outPath.any()),
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

func (e *EmitContext) emitGoExe(resolved []ResolvedPeer, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, peerSbomRefs []NodeRef, peerSbomPaths []VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()
	outName := programBinaryName(instance, d.moduleStmt)
	outPath := build(dir, "/", outName)
	goRes := e.goRes

	if goRes == nil {
		goRes = &GoSrcsResult{}
	}

	vcsCPath := build(dir, "/__vcs_version__.c")
	vcsOPath := build(dir, "/__vcs_version__.c", instance.Platform.objectSuffix())
	vcsGoPath := build(dir, "/__vcs_version__.go")
	cmd0 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsInfo(na, d.tc, vcsCPath)), Env: goVcsEnv}
	cmd1 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsCompileForced(na, instance.Platform, d.tc, vcsCPath, vcsOPath, d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize, true)), Env: instance.Platform.ToolEnvVars}

	cmd2 := Cmd{CmdArgs: na.chunkList(na.anyList(
		wrapccPython3STR.any(),
		strGoVcsInfoScript.any(),
		strOutputGo.any(),
		strGoVcsJson,
		vcsGoPath.any(),
		internStr(goArcPrefix).any(),
	)), Env: goVcsEnv}

	srcArgs := na.anys.alloc(len(goRes.GoFiles))

	for i, src := range goRes.GoFiles {
		srcArgs[i] = src.any()
	}

	na.anys.commit(len(goRes.GoFiles))

	srcArgs = srcArgs[:len(goRes.GoFiles):len(goRes.GoFiles)]

	localARs, nonLocalARs, cgoARs := goClassifyClosure(na, resolved, peerArchivePaths)
	tail := na.anys.alloc(4 + len(localARs) + len(nonLocalARs) + len(cgoARs))
	nt := 0
	pushTail := func(x STR) { tail[nt] = x.any(); nt++ }

	pushTail(goToolCmdPeers[0])

	for _, p := range localARs {
		pushTail(p.rel())
	}

	pushTail(strNonLocalPeers)

	for _, p := range nonLocalARs {
		pushTail(p.rel())
	}

	pushTail(strCgoPeers)
	pushTail(vcsOPath.rel())

	for _, p := range cgoARs {
		pushTail(p.rel())
	}

	na.anys.commit(nt)

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
		na.vfsList(goRes.GoFiles...),
		goToolScriptInputsChunk,
		ctx.scripts[ldVcsInfoVFS.rel()],
		ctx.scripts[ldFsToolsVFS.rel()],
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
		na.anyList(build(dir).any()),
		goToolCmdMid,
		na.anyList(outPath.any()),
		goToolCmdVet,
		srcArgs,
		e.goToolCmdFlagsFor(),
		goExeCmdLinkFlags,
		na.anyList(vcsGoPath.any()),
		goExeCmdExtld,
		goExtldflagsArgs(na, instance.Platform, d.tc, d.useArcadiaLibm),
		tail[:nt:nt],
		goToolCmdEnd,
	), Env: env}

	sbomJSON := build(dir, "/__sbomdata.json")

	var linkSbomCmd, sbomObjcopyCmd Cmd

	if sbomEmbed {
		linkSbomCmd = Cmd{CmdArgs: na.chunkList(composeLDCmdLinkSbom(na, d.tc, strGo, instance.Path.rel(), sbomJSON, peerSbomPaths)), Cwd: bldRootDirVFS, Env: goVcsEnv}
		sbomObjcopyCmd = Cmd{CmdArgs: na.chunkList(composeLDCmdSbomObjcopy(na, d.tc, sbomJSON, outPath)), Env: goVcsEnv}
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
