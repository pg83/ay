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

var goKV = KV{P: pkGO, PC: pcLightRed, ShowOut: true}

var goToolKV = KV{P: pkGoTool, PC: pcLightBlue, ShowOut: true}

var goLdKV = KV{P: pkLD, PC: pcLightRed, ShowOut: true}

func isGoModuleType(name TOK) bool {
	return name == tokGoLibrary || name == tokGoProgram
}

func goImportPathFor(dir string) string {
	if rest, ok := strings.CutPrefix(dir, goStdPrefix+"/vendor/"); ok {
		return rest
	}

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
		addPeer(goStdPrefix + "/runtime")
		addPeer(goStdPrefix + "/runtime/cgo")
		addPeer("library/go/core/buildinfo")
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

	addPeer(goToolsPeer)
	addPeer(goYolintPeer)
}

type GoSrcsResult struct {
	GoFiles    []VFS
	AsmFiles   []VFS
	SymabisRef NodeRef
	SymabisOut VFS
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

func (e *EmitContext) flushGoSrcs() {
	if e.goRes == nil || len(e.goRes.AsmFiles) == 0 {
		return
	}

	ctx, instance := e.ctx, e.instance
	na := ctx.na
	dir := instance.Path.rel()
	out := build(dir, "/gen.symabis")

	args := []STR{
		internV(resourcePatternRef("GO_TOOLS"), "/pkg/tool/linux_amd64/asm"),
		internStr("-trimpath"),
		internStr("$(SOURCE_ROOT)=>/-S;$(BUILD_ROOT)=>/-B;$(TOOL_ROOT)=>/-T"),
		internStr("-I"), bld.str(),
		internStr("-I"), v.str(),
		internStr("-I"), internV("$(S)/", goStdPrefix, "/runtime").str(),
		internStr("-I"), internV("$(S)/", "build/scripts/go_fake_include").str(),
		internStr("-I"), internV("$(S)/", "contrib/libs/linux-headers").str(),
		internStr("-I"), internV("$(S)/", "contrib/libs/linux-headers/_nf").str(),
		internStr("-I"), internV(resourcePatternRef("GO_TOOLS"), "/pkg/include").str(),
		internStr("-D"), internStr("GOOS_linux"),
		internStr("-D"), internStr("GOARCH_amd64"),
		internStr("-p"), internStr(goImportPathFor(dir)),
		internStr("-gensymabis"),
		internStr("-o"), out.str(),
	}

	inputs := make([]VFS, 0, len(e.goRes.AsmFiles)+1)

	for _, src := range e.goRes.AsmFiles {
		args = append(args, src.str())
		inputs = append(inputs, src)
	}

	inputs = append(inputs, source(goStdPrefix+"/runtime/textflag.h"))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args), Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputs),
		KV:           &goToolKV,
		Outputs:      na.vfsList(out),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    goToolResources(e.peers.ResourceGlobals),
	}

	e.goRes.SymabisRef = ctx.emit.emitNode(node)
	e.goRes.SymabisOut = out
}

func goToolResources(decls []ResourceDecl) []STR {
	out := make([]STR, 0, len(decls))

	for _, d := range decls {
		out = append(out, d.Name)
	}

	return out
}

func goToolCmdHead(mode, dir string, outPath VFS) []STR {
	return []STR{
		wrapccPython3STR,
		internV("$(S)/", "build/scripts/go_tool.py").str(),
		internStr("--ya-start-command-file"),
		internStr("++mode"), internStr(mode),
		internStr("++std-lib-prefix"), internStr(goStdPrefix + "/"),
		internStr("++arc-project-prefix"), internStr(goArcPrefix),
		internStr("++goversion"), internStr(goVersion),
		internStr("++lang"),
		internStr("++source-root"), v.str(),
		internStr("++build-root"), bld.str(),
		internStr("++output-root"), build(dir).str(),
		internStr("++toolchain-root"), internStr(resourcePatternRef("GO_TOOLS")),
		internStr("++host-os"), internStr("linux"),
		internStr("++host-arch"), internStr("amd64"),
		internStr("++targ-os"), internStr("linux"),
		internStr("++targ-arch"), internStr("amd64"),
		internStr("++output"), outPath.str(),
		internStr("++vet"), internV(resourcePatternRef("YOLINT"), "/yolint").str(),
		internStr("++vet-flags"),
		internV("-migration.config=", "$(S)/", "build/rules/go/migrations.yaml").str(),
		internV("-scopelint.config=", "$(S)/", "build/rules/go/extended_lint.yaml").str(),
		internV("-riskyimports.config=", "$(S)/", "build/rules/go/risky_imports.yaml").str(),
		internStr("++debug-root-map"), internStr("source=/-S;build=/-B;tools=/-T"),
		internStr("++tools-root"), internStr("$(TOOL_ROOT)"),
		internStr("++srcs"),
	}
}

func goToolScriptInputs() []VFS {
	return []VFS{
		source("build/scripts/go_tool.py"),
		source("build/scripts/process_command_files.py"),
		source("build/scripts/process_whole_archive_option.py"),
		source("build/rules/go/migrations.yaml"),
		source("build/rules/go/extended_lint.yaml"),
		source("build/rules/go/risky_imports.yaml"),
	}
}

func goCmdEnv() EnvVars {
	return EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS},
		{Name: envGOARCH, Value: internStr("amd64")},
		{Name: envGOOS, Value: internStr("linux")},
	}
}

func goDirectPeerARs(resolved []resolvedPeer) ([]NodeRef, []VFS) {
	refs := make([]NodeRef, 0, len(resolved))
	paths := make([]VFS, 0, len(resolved))

	for _, rp := range resolved {
		if rp.result.ARPath != nil && isGoModuleType(rp.result.ModuleStmtName) {
			refs = append(refs, rp.result.ARRef)
			paths = append(paths, *rp.result.ARPath)
		}
	}

	return refs, paths
}

func (e *EmitContext) emitGoPackage(resolved []resolvedPeer, objRefs []NodeRef, objOuts []VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	dir := instance.Path.rel()
	outName := dir[strings.LastIndexByte(dir, '/')+1:]
	outPath := build(dir, "/", outName, ".a")
	goRes := e.goRes

	if goRes == nil {
		goRes = &GoSrcsResult{}
	}

	args := goToolCmdHead("lib", dir, outPath)

	inputs := make([]VFS, 0, 64)

	if goRes.SymabisRef != 0 {
		args = append(args, goRes.SymabisOut.str())
		inputs = append(inputs, goRes.SymabisOut)
	}

	for _, o := range objOuts {
		args = append(args, o.str())
		inputs = append(inputs, o)
	}

	for _, src := range goRes.GoFiles {
		args = append(args, src.str())
		inputs = append(inputs, src)
	}

	for _, src := range goRes.AsmFiles {
		args = append(args, src.str())
		inputs = append(inputs, src)
	}

	args = append(args,
		internStr("++asm-flags"),
		internStr("++compile-flags"),
		internStr("++link-flags"),
		internStr("-linkmode=external"),
		internStr("++cgo-srcs"),
		internStr("++peers"),
	)

	peerRefs, peerARs := goDirectPeerARs(resolved)

	peerRefs = append(append(make([]NodeRef, 0, len(objRefs)+len(peerRefs)), objRefs...), peerRefs...)

	for _, p := range peerARs {
		args = append(args, internStr(p.rel()))
	}

	args = append(args, internStr("--ya-end-command-file"))

	inputs = append(inputs, goToolScriptInputs()...)
	inputs = append(inputs, peerARs...)

	env := goCmdEnv()

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args), Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputs),
		KV:           &goKV,
		Outputs:      na.vfsList(outPath, build(dir, "/", outName, ".a.vet.out"), build(dir, "/", outName, ".a.vet.txt")),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      append(depRefs(goRes.SymabisRef), peerRefs...),
		Resources:    goToolResources(resourceGlobals),
	}

	return ctx.emit.emitNode(node), outPath
}

func (e *EmitContext) emitGoExe(resolved []resolvedPeer, peerArchiveRefs []NodeRef, peerArchivePaths []VFS, resourceGlobals []ResourceDecl) (NodeRef, VFS) {
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
	vcsOPath := build(dir, "/__vcs_version__.c.o")
	vcsGoPath := build(dir, "/__vcs_version__.go")

	vcsEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	cmd0 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsInfo(d.tc, vcsCPath.string())), Env: vcsEnv}
	cmd1 := Cmd{CmdArgs: na.chunkList(composeLDCmdVcsCompile(instance.Platform, d.tc, vcsCPath.string(), vcsOPath.string(), d.cFlags, nil, d.moduleScopeCFlags, d.flags.NoCompilerWarnings, d.noOptimize)), Env: vcsEnv}
	cmd2 := Cmd{CmdArgs: na.chunkList(na.strList(
		wrapccPython3STR,
		internV("$(S)/", "build/scripts/vcs_info.py").str(),
		internStr("output-go"),
		internV("$(VCS)/", "vcs.json").str(),
		vcsGoPath.str(),
		internStr(goArcPrefix),
	)), Env: vcsEnv}

	args := goToolCmdHead("exe", dir, outPath)

	inputs := make([]VFS, 0, 128)

	for _, src := range goRes.GoFiles {
		args = append(args, src.str())
		inputs = append(inputs, src)
	}

	args = append(args,
		internStr("++asm-flags"),
		internStr("++compile-flags"),
		internStr("++link-flags"),
		internStr("-linkmode=external"),
		internStr("++cgo-srcs"),
		internStr("++ld_plugins"),
		internStr("++vcs"), vcsGoPath.str(),
		internStr("++extld"), internStr("clang"),
		internStr("++extldflags"),
	)

	args = append(args, internStr("++peers"))

	directRefs, directARs := goDirectPeerARs(resolved)

	_ = directRefs

	for _, p := range directARs {
		args = append(args, internStr(p.rel()))
	}

	args = append(args, internStr("++non-local-peers"))

	deduper.reset()

	for _, p := range directARs {
		deduper.add(p.strID())
	}

	var cgoPeerPaths []VFS

	for _, p := range peerArchivePaths {
		if !deduper.add(p.strID()) {
			continue
		}

		if isGoArchivePath(p.rel()) {
			args = append(args, internStr(p.rel()))
		} else {
			cgoPeerPaths = append(cgoPeerPaths, p)
		}
	}

	args = append(args, internStr("++cgo-peers"), internStr(vcsOPath.rel()))

	for _, p := range cgoPeerPaths {
		args = append(args, internStr(p.rel()))
	}

	args = append(args, internStr("--ya-end-command-file"))

	inputs = append(inputs, goToolScriptInputs()...)
	inputs = append(inputs, peerArchivePaths...)

	env := goCmdEnv()
	goCmd := Cmd{CmdArgs: na.chunkList(args), Env: env}

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(cmd0, cmd1, cmd2, goCmd),
		Env:          env,
		Inputs:       na.inputList(inputs),
		KV:           &goLdKV,
		Outputs:      na.vfsList(outPath, build(dir, "/", outName, ".vet.txt")),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      peerArchiveRefs,
		Resources:    goToolResources(resourceGlobals),
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
