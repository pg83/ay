package main

import (
	"path"
	"slices"
	"strings"
)

var (
	goKV                 = KV{P: pkGO, PC: pcLightRed, ShowOut: true}
	goToolKV             = KV{P: pkGoTool, PC: pcLightBlue, ShowOut: true}
	goLdKV               = KV{P: pkLD, PC: pcLightRed, ShowOut: true}
	goVcsEnv             = envVarsVCS
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
	goToolCmdFlags       = goToolCmdFlagsChunk(false)
	goToolCmdFlagsPic    = goToolCmdFlagsChunk(true)
	goToolCmdPeers       = []STR{internStr("++peers")}
	goToolCmdEnd         = []ANY{strYaEndCommandFile.any()}
	goExeCmdExtld        = []ANY{internStr("++extld").any(), strClang.any(), internStr("++extldflags").any()}
)

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
}

var goExeCmdVcs = []ANY{
	internStr("++ld_plugins").any(),
	internStr("++vcs").any(),
}

var goExtldWholeArchive = []ANY{
	internStr("-Wl,--whole-archive").any(),
	internStr("-Wl,--no-whole-archive").any(),
	internStr("--cgo-peers").any(),
}

var goExtldLinkerTail = []ANY{
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
	strD2.any(), internStr("GOOS_linux").any(),
	strD2.any(), internStr("GOARCH_amd64").any(),
	internStr("-p").any(),
}

const (
	goConfRel       = "build/conf/go.conf"
	ldConfRel       = "build/conf/linkers/ld.conf"
	mergeSdcmetaRel = "build/internal/scripts/merge_sdcmeta.py"
	goArcPrefix     = "a.yandex-team.ru/"
	goToolsPeer     = "build/external_resources/go_tools"
	goYolintPeer    = "build/external_resources/yolint"
	goVendorPrefix  = "vendor"
)

type GoStd struct {
	Version        string
	Prefix         string
	Runtime        VFS
	AsmIncludeDirs []VFS
	HeadLib        []ANY
	HeadExe        []ANY
	HeadTest       []ANY
	HeadDll        []ANY
}

func confGoStdVersion(fs FS) string {
	if !fs.isFile(srcRootRel, goConfRel) {
		return ""
	}

	for _, line := range strings.Split(string(concatChunks(fs.read(goConfRel))), "\n") {
		if v, ok := strings.CutPrefix(line, "GOSTD_VERSION="); ok {
			return strings.TrimSpace(v)
		}
	}

	return ""
}

func newGoStd(version string) *GoStd {
	g := &GoStd{Version: version, Prefix: "contrib/go/_std_" + version + "/src"}

	if version == "" {
		g.Prefix = "__unsupported_go_std_library_version_[]__"
	}

	g.Runtime = source(g.Prefix + "/runtime")
	g.AsmIncludeDirs = []VFS{g.Runtime, goFakeIncludeVFS, contribLibsLinuxHeaders, contribLibsLinuxHeadersNf}
	g.HeadLib = g.toolCmdHead("lib")
	g.HeadExe = g.toolCmdHead("exe")
	g.HeadTest = g.toolCmdHead("test")
	g.HeadDll = g.toolCmdHead("dll")

	if ownershipOn {
		registerOwnedSlice(g.AsmIncludeDirs)
		registerOwnedSlice(g.HeadLib)
		registerOwnedSlice(g.HeadExe)
		registerOwnedSlice(g.HeadTest)
		registerOwnedSlice(g.HeadDll)
	}

	return g
}

func (g *GoStd) toolCmdHead(mode string) []ANY {
	return []ANY{
		wrapccPython3STR.any(),
		strGoToolScript.any(),
		strYaStartCommandFile.any(),
		strMode.any(), internStr(mode).any(),
		strStdLibPrefix.any(), internStr(g.Prefix + "/").any(),
		strArcProjectPrefix.any(), internStr(goArcPrefix).any(),
		strGoversion.any(), internStr(g.Version).any(),
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
	return name == tokGoLibrary || isGoExeModuleType(name)
}

func goSrcsExtensionSupported(ext string) bool {
	switch ext {
	case ".c", ".cc", ".cpp", ".cxx", ".C", ".S", ".ev", ".fbs", ".go", ".in", ".proto", ".s", ".syso":
		return true
	}

	return false
}

func isGoExeModuleType(name TOK) bool {
	return name == tokGoProgram || name == tokGoTest || name == tokGoDll
}

func (g *GoStd) importPath(dir string) string {
	if rest, ok := strings.CutPrefix(dir, g.Prefix+"/"); ok {
		return rest
	}

	if rest, ok := strings.CutPrefix(dir, goVendorPrefix+"/"); ok {
		return rest
	}

	return goArcPrefix + dir
}

func goImportDir(ctx *GenCtx, std *GoStd, importerDir, imp string) string {
	if imp == "unsafe" || imp == "C" {
		return ""
	}

	if rest, ok := strings.CutPrefix(imp, goArcPrefix); ok {
		imp = rest
	}

	candidates := []string{goVendorPrefix + "/" + imp, std.Prefix + "/" + imp, imp}

	if strings.HasPrefix(importerDir, std.Prefix+"/") {
		candidates = []string{std.Prefix + "/vendor/" + imp, std.Prefix + "/" + imp, imp}
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
	std := instance.Platform.Go

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

	d.peerdirs = append(slices.Clip(d.goCommonGoogleApis), d.peerdirs...)

	if isGoExeModuleType(d.moduleStmt.Name) {
		user := slices.Clone(d.peerdirs)

		d.peerdirs = d.peerdirs[:0]

		addPeer(std.Prefix + "/runtime/cgo")

		if d.moduleStmt.Name == tokGoTest {
			addPeer(std.Prefix + "/internal/coverage/cfile")
			addPeer(std.Prefix + "/runtime/coverage")
		}

		addPeer(std.Prefix + "/runtime")
		addPeer("library/go/core/buildinfo")

		if d.moduleStmt.Name == tokGoTest {
			addPeer(std.Prefix + "/os")
			addPeer(std.Prefix + "/testing/internal/testdeps")
			addPeer(std.Prefix + "/testing")
			addPeer("library/go/test/yatest")
		}

		for _, p := range user {
			if !slices.Contains(d.peerdirs, p) {
				d.peerdirs = append(d.peerdirs, p)
			}
		}
	}

	if d.hasFbs {
		addPeer(std.Prefix + "/bytes")
		addPeer(std.Prefix + "/strconv")
		addPeer("vendor/github.com/google/flatbuffers/go")
	}

	if goModuleHasProtos(d) {
		for _, p := range goProtobufImports(std) {
			addPeer(p)
		}
	}

	for _, p := range d.goPluginPeers {
		addPeer(p.string())
	}

	if len(d.cgoSrcs) > 0 {
		addPeer("build/internal/platform/clang_toolchain_info")
		addPeer("build/platform/lld")
		addPeer(std.Prefix + "/runtime/cgo")
	}

	addImports := func(src ANY) {
		path := src.vfs()

		if path == 0 {
			path = resolveSourceVFS(ctx, instance, src.string(), d.srcDirs)
		}

		if path.isBuild() {
			return
		}

		// Upstream missing-file nodes carry no parsed imports. Keep the input
		// for emission, where CodegenRegistry resolves declared producers.
		if present, _ := ctx.fs.exists(srcRootRel, path.relString()); !present {
			return
		}

		for _, imp := range parseGoImports(concatChunks(ctx.fs.read(path.relString()))) {
			addPeer(goImportDir(ctx, std, dir, imp.string()))
		}
	}

	for _, src := range d.cgoSrcs {
		addImports(src)
	}

	for _, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		if strings.HasSuffix(meta.Source.relOrSelf().string(), ".go") {
			addImports(meta.Source)
		}
	}

	for _, srcs := range [][]ANY{d.goTestSrcs, d.goXTestSrcs} {
		for _, src := range srcs {
			addImports(src)
		}
	}

	addPeer(goToolsPeer)
	addPeer(goYolintPeer)

	goIncl := make([]VFS, 0, 3)

	goIncl = append(goIncl, std.Runtime)

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
		d.addIncl = append(d.addIncl, instance.Path)
	}

	d.addIncl = append(goIncl, d.addIncl...)

	cgoFlags := goCgoCFlags(d)

	for i := range d.srcs {
		meta := &d.srcs[i]

		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		ext := path.Ext(meta.Source.string())

		if ext == ".S" || slices.Contains(goCgoExportExts, ext) {
			meta.Compile.CFlags = concat(meta.Compile.CFlags, cgoFlags)
		}
	}
}

type GoSrcsResult struct {
	AutoFiles   []VFS
	AutoSeqs    []int
	GoFiles     []VFS
	AsmFiles    []VFS
	AsmInclSrcs []VFS
	views       []Closure
	SymabisRef  NodeRef
	SymabisOut  VFS
}

func (e *EmitContext) collectGoAutoSource(out VFS, seq int) {
	if e.goRes == nil {
		e.goRes = &GoSrcsResult{}
	}

	r := e.goRes
	i := len(r.AutoSeqs)

	for i > 0 && r.AutoSeqs[i-1] > seq {
		i--
	}

	r.AutoFiles = slices.Insert(r.AutoFiles, i, out)
	r.AutoSeqs = slices.Insert(r.AutoSeqs, i, seq)
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

	return block[:k:k]
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

	push(internStr(instance.Platform.Go.importPath(dir)))

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

	node := Node{
		Platform:      instance.Platform,
		Cmds:          na.cmdList(Cmd{CmdArgs: na.chunkList(goSymabisHead, e.goInclSplitArgs(), goSymabisDefs, tail[:nt:nt]), Env: goVcsEnv}),
		Env:           goVcsEnv,
		Inputs:        na.inputList(na.vfsList(e.goRes.AsmFiles...), e.goRes.AsmInclSrcs),
		InputClosures: e.goRes.views,
		KV:            &goToolKV,
		Outputs:       na.vfsList(out),
		Resources:     goToolResources(na, e.peers.ResourceGlobals),
	}

	e.goRes.SymabisRef = e.emitNode(node)
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
		e.goRes.views = append(e.goRes.views, cv)

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

func (e *EmitContext) goClassifyClosure(resolved []ResolvedPeer, closurePaths []VFS) (localARs, nonLocalARs, cgoARs []VFS) {
	na := e.ctx.na

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
			case e.isGoArchive(p):
				return 1
			case e.codegen.moduleType(p) == tokGenLibrary:
				return 3
			default:
				return 2
			}
		}

		var counts [4]int

		for _, p := range closurePaths {
			counts[classify(p)]++
		}

		block := na.vfs.alloc(len(closurePaths))
		starts := [4]int{0, counts[0], counts[0] + counts[1], counts[0] + counts[1] + counts[2]}
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

func goExtldflagsArgs(na *NodeArenas, p *Platform, tc ModuleToolchain, useArcadiaLibm bool, soname string, globals []VFS, ldFlagsGlobal, objAddLibsGlobal []ANY) []ANY {
	gdb := p.linkerSelectionGDBIndexFlags()
	objAddLibs := appendArgGroupStr(nil, objAddLibsGlobal)
	bound := len(objAddLibs) + 2 + len(p.SysrootArgs) + len(goExtldWholeArchive) + len(globals) + 1 + len(p.LinkPreludeExtra) + 1 + 2 + len(gdb) + len(ldFlagsGlobal) + len(goExtldLinkerTail) + len(p.Linker.SystemLibs) + 1
	block := na.anys.alloc(bound)
	k := 0
	push := func(x ANY) { block[k] = x; k++ }

	if soname != "" {
		push(internV("-Wl,-soname,", soname).any())
	}

	push(p.TargetArg.any())

	for _, x := range p.SysrootArgs {
		push(x)
	}

	push(goExtldWholeArchive[0])

	for _, g := range globals {
		push(g.rel().any())
	}

	push(goExtldWholeArchive[1])
	push(goExtldWholeArchive[2])

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

	for _, x := range ldFlagsGlobal {
		push(x)
	}

	for _, x := range objAddLibs {
		push(x)
	}

	for _, x := range goExtldLinkerTail {
		push(x)
	}

	for _, x := range p.Linker.SystemLibs {
		push(x)
	}

	if !useArcadiaLibm {
		push(argDashLm.any())
	}

	na.anys.commit(k)

	return block[:k:k]
}

func (e *EmitContext) goToolchainSboms() ([]NodeRef, []VFS) {
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

	add(pythonToolchainSbomComponent(ctx, instance.Platform))

	return refs, paths
}

func (e *EmitContext) goToolCmdFlagsFor() []ANY {
	if e.instance.Platform.PIC {
		return goToolCmdFlagsPic
	}

	return goToolCmdFlags
}

func (e *EmitContext) goIncludedSrcs(goRes *GoSrcsResult) []VFS {
	return e.goCgoClosure(goRes, goRes.AsmInclSrcs)
}

func (e *EmitContext) goCgoClosure(goRes *GoSrcsResult, includedSrcs []VFS) []VFS {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.cgoSrcs) > 0 {
		cvs := e.cvScratch[:0]
		extrasCap := len(includedSrcs)

		for _, f := range d.cgoSrcs {
			cv := e.scanner.walkClosure(resolveSourceVFS(ctx, instance, f.string(), d.srcDirs), d.scanCtx, scanDomainCC)
			goRes.views = append(goRes.views, cv)

			extrasCap += cv.len()
			cvs = append(cvs, cv)
		}

		e.cvScratch = retainMaxLen(e.cvScratch, cvs)

		block := na.vfs.alloc(extrasCap)
		k := copy(block, includedSrcs)

		dedupers.with(func(deduper *DeDuper) {
			for _, p := range includedSrcs {
				deduper.add(p.strID())
			}

			for _, cv := range cvs {
				cv.each(func(p VFS) {
					if p.isSource() && deduper.add(p.strID()) {
						block[k] = p
						k++
					}
				})
			}
		})

		na.vfs.commit(k)

		includedSrcs = block[:k:k]
	}

	return includedSrcs
}

func (e *EmitContext) goSrcList(goRes *GoSrcsResult, objOuts []VFS) (srcArgs, cgoSrcArgs []ANY, ownInputs, ownSources, ownBuilds []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()
	srcCap := 1 + len(objOuts) + len(goRes.AutoFiles) + len(goRes.GoFiles) + len(goRes.AsmFiles) + len(d.cgoSrcs)

	srcArgs = na.anys.alloc(srcCap)

	srcInputs := na.vfs.alloc(srcCap)

	ownSources = make([]VFS, 0, srcCap)
	ownBuilds = make([]VFS, 0, srcCap)

	nsrc := 0

	addSrc := func(p VFS) {
		srcArgs[nsrc] = p.any()
		srcInputs[nsrc] = p
		nsrc++

		if p.isBuild() {
			ownBuilds = append(ownBuilds, p)
		} else {
			ownSources = append(ownSources, p)
		}
	}

	for _, src := range goRes.AutoFiles {
		addSrc(src)
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

		addSrc(cgoSrc)
	}

	na.anys.commit(nsrc)
	na.vfs.commit(nsrc)

	plain := nsrc - len(d.cgoSrcs)

	cgoSrcArgs = srcArgs[plain:nsrc:nsrc]
	srcArgs = srcArgs[:plain:plain]
	ownInputs = srcInputs[:nsrc:nsrc]

	return srcArgs, cgoSrcArgs, ownInputs, ownSources, ownBuilds
}

func (e *EmitContext) emitGoPackage(resolved []ResolvedPeer, objRefs []NodeRef, objOuts []VFS, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, peerGlobalRefs []NodeRef, peerGlobalPaths []VFS, peerSbomRefs []NodeRef, peerSbomPaths []VFS, ownSbomRef *NodeRef, ownSbomPath *VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()
	outName := programBinaryName(instance, d.moduleStmt)
	outPath := build(dir, "/", outName, ".a")
	goRes := e.goRes

	if goRes == nil {
		goRes = &GoSrcsResult{}
	}

	srcArgs, cgoSrcArgs, ownInputs, ownSources, ownBuilds := e.goSrcList(goRes, objOuts)
	localARs, _, _ := e.goClassifyClosure(resolved, peerArchivePaths)
	embedArgs, embedInputs := goEmbedArgs(na, d.goEmbeds)
	tail := na.anys.alloc(4 + len(cgoSrcArgs) + len(embedArgs) + len(localARs))
	nt := 0
	pushTail := func(x STR) { tail[nt] = x.any(); nt++ }

	pushTail(strLinkFlags)
	pushTail(strLinkmodeExternal)
	pushTail(strCgoSrcs)

	nt += copy(tail[nt:], cgoSrcArgs)
	nt += copy(tail[nt:], embedArgs)

	pushTail(goToolCmdPeers[0])

	for _, p := range localARs {
		pushTail(p.rel())
	}

	na.anys.commit(nt)

	includedSrcs := e.goIncludedSrcs(goRes)
	ownSourceInputs := na.vfsList(ownSources...)
	ownBuildInputs := na.vfsList(ownBuilds...)
	sbomRefs, sbomPaths := e.goToolchainSboms()

	var mergedSbomRefs []NodeRef
	var merged int
	var extraSources, extraBuilds []VFS

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

		extraSourceBlock := na.vfs.alloc(len(includedSrcs) + 1)[:0]

		for _, p := range includedSrcs {
			if deduper.add(p.strID()) {
				extraSourceBlock = append(extraSourceBlock, p)
			}
		}

		na.vfs.commit(len(extraSourceBlock))
		extraSources = extraSourceBlock[:len(extraSourceBlock):len(extraSourceBlock)]

		extraBuildBlock := na.vfs.alloc(1 + merged + len(ownBuilds))[:0]

		if len(d.cgoSrcs) > 0 {
			extraBuildBlock = append(extraBuildBlock, build(dir, "/_cgo_main.c", instance.Platform.objectSuffix()))
		}

		for _, b := range ownBuilds {
			if info := e.codegen.lookup(b); info != nil && info.ProducerMainOut != 0 && deduper.add(info.ProducerMainOut.strID()) {
				extraBuildBlock = append(extraBuildBlock, info.ProducerMainOut)
			}
		}

		for _, p := range mergedSbomPaths[:merged] {
			extraBuildBlock = append(extraBuildBlock, p)
		}

		na.vfs.commit(len(extraBuildBlock))
		extraBuilds = extraBuildBlock[:len(extraBuildBlock):len(extraBuildBlock)]
	})

	inputs := na.inputList(ownSourceInputs, ownBuildInputs, goToolScriptInputsChunk, embedInputs, peerArchivePaths, peerGlobalPaths, extraSources, extraBuilds)
	depBlock := na.noderefs.alloc(1 + len(objRefs) + len(peerArchiveRefs) + len(peerGlobalRefs) + merged)
	ndep := 0

	if goRes.SymabisRef != 0 {
		depBlock[ndep] = goRes.SymabisRef
		ndep++
	}

	ndep += copy(depBlock[ndep:], objRefs)
	ndep += copy(depBlock[ndep:], peerArchiveRefs)
	ndep += copy(depBlock[ndep:], peerGlobalRefs)
	ndep += copy(depBlock[ndep:], mergedSbomRefs[:merged])

	na.noderefs.commit(ndep)

	env := goCmdEnv(ctx, instance.Platform, d.tc)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(
			instance.Platform.Go.HeadLib,
			na.anyList(build(dir).any()),
			goToolCmdMid,
			na.anyList(outPath.any()),
			goToolCmdVet,
			srcArgs,
			e.goToolCmdFlagsFor(),
			tail[:nt:nt],
			goToolCmdEnd,
		), Env: env}),
		Env:           env,
		Inputs:        inputs,
		InputClosure:  Closure{self: outPath},
		InputClosures: goRes.views,
		KV:            &goKV,
		Outputs:       na.vfsList(outPath, build(dir, "/", outName, ".a.vet.out"), build(dir, "/", outName, ".a.vet.txt")),
		DepRefs:       depBlock[:ndep],
		Resources:     goToolResources(na, resourceGlobals),
	}

	ref := e.emitModuleNode(node, false)

	e.register(GeneratedFileInfo{OutputPath: outPath, InputsOnly: true, ModuleType: d.moduleStmt.Name})

	return ref, outPath
}

func (e *EmitContext) emitGoExe(resolved []ResolvedPeer, objRefs []NodeRef, objOuts []VFS, peerObjAddLibsGlobal []ANY, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, peerGlobalRefs []NodeRef, peerGlobalPaths []VFS, peerSbomRefs []NodeRef, peerSbomPaths []VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()
	outName := programBinaryName(instance, d.moduleStmt)
	isDll := d.moduleStmt.Name == tokGoDll
	soname := ""

	if isDll {
		outName = dllOutputName(instance, d.moduleStmt)
		soname = outName
	}

	outPath := build(dir, "/", outName)
	goRes := e.goRes

	if goRes == nil {
		goRes = &GoSrcsResult{}
	}

	vcsCPath := build(dir, "/__vcs_version__.c")
	vcsOPath := build(dir, "/__vcs_version__.c", instance.Platform.objectSuffix())
	vcsGoPath := build(dir, "/__vcs_version__.go")
	cmd0 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsInfo(na, d.tc, vcsCPath)), Env: goVcsEnv}
	cmd1 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsCompileForced(na, instance.Platform, d.tc, vcsCPath, vcsOPath, d.cFlags, d.cc.PeerCFlagsGlobal, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize, true)), Env: instance.Platform.ToolEnvVars}

	cmd2 := Cmd{CmdArgs: na.chunkList(na.anyList(
		wrapccPython3STR.any(),
		strGoVcsInfoScript.any(),
		strOutputGo.any(),
		strGoVcsJson,
		vcsGoPath.any(),
		internStr(goArcPrefix).any(),
	)), Env: goVcsEnv}

	srcArgs, cgoSrcArgs, ownInputs, _, _ := e.goSrcList(goRes, objOuts)
	localARs, nonLocalARs, cgoARs := e.goClassifyClosure(resolved, peerArchivePaths)
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

	isTest := d.moduleStmt.Name == tokGoTest
	head, kv := instance.Platform.Go.HeadExe, &goLdKV

	if isDll {
		head = instance.Platform.Go.HeadDll
	}

	var testArgs, testTail []ANY
	var testInputs []VFS
	var minerRef NodeRef

	if isTest {
		var minerPath VFS

		head, kv = instance.Platform.Go.HeadTest, &goKV
		minerRef, minerPath = ctx.tool(argToolsGoTestMiner)
		testArgs = []ANY{strTestMiner.any(), minerPath.any(), strTestImportPath.any()}

		if imp := d.setVars[strGO_TEST_IMPORT_PATH]; imp != 0 {
			testArgs = append(testArgs, imp.any())
		}

		testTail = append(testTail, strTestSrcs.any())

		for _, src := range d.goTestSrcs {
			p := resolveSourceVFS(ctx, instance, src.string(), d.srcDirs)

			testTail = append(testTail, p.any())
			testInputs = append(testInputs, p)
		}

		testTail = append(testTail, strXTestSrcs.any())

		for _, src := range d.goXTestSrcs {
			p := resolveSourceVFS(ctx, instance, src.string(), d.srcDirs)

			testTail = append(testTail, p.any())
			testInputs = append(testInputs, p)
		}

		testTail = append(testTail, strSkipTests.any())
		testTail = append(testTail, d.goSkipTests...)

		testInputs = append(testInputs, minerPath)
	}

	hasSbom := sbomActive(ctx, instance) && len(peerSbomPaths) > 0
	sbomEmbed := !isTest && instance.Platform.BuildRelease && hasSbom
	extraSources := na.vfsList(ldSvnInterfaceVFS, ldSvnversionHVFS)

	var extraBuilds, extraTailSources []VFS

	if hasSbom {
		extraBuilds = na.vfsList(peerSbomPaths...)
	}

	if sbomEmbed {
		extraTailSources = na.vfsList(linkSbomScriptVFS)
	}

	var dllInputs []VFS

	if isDll {
		dllInputs = ctx.scripts[copyFsToolsVFS.rel()]
	}

	embedArgs, embedInputs := goEmbedArgs(na, d.goEmbeds)

	var cgoBuilds []VFS

	if len(d.cgoSrcs) > 0 {
		cgoBuilds = na.vfsList(build(dir, "/_cgo_main.c", instance.Platform.objectSuffix()))
	}

	pluginArgs := na.anys.alloc(len(e.moduleGlobals))[:0]

	for _, input := range e.moduleGlobals {
		if strings.HasSuffix(input.relString(), ".pyplugin") {
			pluginArgs = append(pluginArgs, input.any())
		}
	}

	na.anys.commit(len(pluginArgs))
	pluginArgs = pluginArgs[:len(pluginArgs):len(pluginArgs)]

	inputs := na.inputList(
		ownInputs,
		e.goIncludedSrcs(goRes),
		cgoBuilds,
		testInputs,
		embedInputs,
		goToolScriptInputsChunk,
		dllInputs,
		peerGlobalPaths,
		ctx.scripts[ldVcsInfoVFS.rel()],
		peerArchivePaths,
		extraSources,
		extraBuilds,
		extraTailSources,
	)

	deps := na.noderefs.alloc(len(objRefs) + len(peerArchiveRefs) + len(peerGlobalRefs) + len(peerSbomRefs) + 2)
	k := copy(deps, objRefs)

	k += copy(deps[k:], peerArchiveRefs)

	k += copy(deps[k:], peerGlobalRefs)

	if hasSbom {
		k += copy(deps[k:], peerSbomRefs)
	}

	deps[k] = ctx.vcsRef
	k++

	if minerRef != 0 {
		deps[k] = minerRef
		k++
	}

	na.noderefs.commit(k)

	env := goCmdEnv(ctx, instance.Platform, d.tc)

	goCmd := Cmd{CmdArgs: na.chunkList(
		head,
		na.anyList(build(dir).any()),
		goToolCmdMid,
		na.anyList(outPath.any()),
		goToolCmdVet,
		srcArgs,
		e.goToolCmdFlagsFor(),
		goExeCmdLinkFlags,
		cgoSrcArgs,
		embedArgs,
		goExeCmdVcs[:1],
		pluginArgs,
		goExeCmdVcs[1:],
		na.anyList(vcsGoPath.any()),
		goExeCmdExtld,
		goExtldflagsArgs(na, instance.Platform, d.tc, d.useArcadiaLibm, soname, peerGlobalPaths, e.peers.LDFlagsGlobal, peerObjAddLibsGlobal),
		testArgs,
		tail[:nt:nt],
		testTail,
		goToolCmdEnd,
	), Env: env}

	sbomJSON := build(dir, "/__sbomdata.json")

	var linkSbomCmd, sbomObjcopyCmd Cmd

	if sbomEmbed {
		linkSbomCmd = Cmd{CmdArgs: na.chunkList(composeLDCmdLinkSbom(na, d.tc, strGo, instance.Path.rel(), sbomJSON, peerSbomPaths)), Cwd: bldRootDirVFS, Env: goVcsEnv}
		sbomObjcopyCmd = Cmd{CmdArgs: na.chunkList(composeLDCmdSbomObjcopy(na, d.tc, sbomJSON, outPath)), Env: goVcsEnv}
	}

	cmds := na.cmds.alloc(8)
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

	outputs := na.vfsList(outPath, build(dir, "/", outName, ".vet.txt"))

	if isDll {
		header := build(dir, "/", strings.TrimSuffix(outName, ".so"), ".h")

		pushCmd(Cmd{CmdArgs: na.chunkList(na.anyList(d.tc.Python3.any(), copyFsToolsVFS.any(), strFsToolsCopy.any(), build(dir, "/_cgo_export.h").any(), header.any())), Env: goVcsEnv})
		outputs = na.vfsList(outPath, build(dir, "/", outName, ".vet.txt"), header)
	}

	na.cmds.commit(kc)

	node := Node{
		Platform:      instance.Platform,
		Cmds:          cmds[:kc:kc],
		Env:           env,
		Inputs:        inputs,
		InputClosure:  Closure{self: outPath},
		InputClosures: goRes.views,
		KV:            kv,
		Outputs:       outputs,
		DepRefs:       deps[:k:k],
		Resources:     goToolResources(na, resourceGlobals),
	}

	if !isTest {
		e.appendSdcMeta(&node, programBinaryName(instance, d.moduleStmt))
	}

	return e.emitModuleNode(node, true), outPath
}

func (e *EmitContext) isGoArchive(p VFS) bool {
	return isGoModuleType(e.codegen.moduleType(p))
}
