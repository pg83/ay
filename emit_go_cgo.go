package main

import (
	"strings"
)

var (
	cgo1WrapperVFS = source("build/scripts/cgo1_wrapper.py")
	linkOScriptVFS = source("build/scripts/link_o.py")
)

func goModuleCgoCFiles(d *ModuleData) []STR {
	var out []STR

	for _, src := range d.srcs {
		rel := src.string()

		if strings.HasSuffix(rel, ".c") || strings.HasSuffix(rel, ".cxx") {
			out = append(out, src)
		}
	}

	return out
}

func goModuleCgoSFiles(d *ModuleData) []STR {
	var out []STR

	for _, src := range d.srcs {
		if strings.HasSuffix(src.string(), ".S") {
			out = append(out, src)
		}
	}

	return out
}

func goModuleUsesCgoC(d *ModuleData) bool {
	return len(goModuleCgoCFiles(d))+len(goModuleCgoSFiles(d))+len(d.cgoSrcs) > 0
}

func goCgoCFlags(d *ModuleData) []ARG {
	out := internArgs([]string{"-w", "-pthread", "-fpic"})

	for _, f := range d.cgoCflags {
		out = append(out, internArgSTR(f))
	}

	return out
}

func (e *EmitContext) goCgoIncludeArgs() []STR {
	d := e.d
	out := make([]STR, 0, len(d.cc.AddIncl)+len(d.cc.PeerAddInclGlobal))

	out = append(out, internStr("-I$(B)"), internStr("-I$(S)"))

	deduper.reset()

	for _, p := range d.cc.AddIncl {
		if deduper.add(p.strID()) {
			out = append(out, d.cc.InclArgs.arg(p))
		}
	}

	for _, p := range d.cc.PeerAddInclGlobal {
		if deduper.add(p.strID()) {
			out = append(out, d.cc.InclArgs.arg(p))
		}
	}

	return out
}

func goCgoImportPathFlags(dir string) (string, string) {
	if dir == goStdPrefix+"/runtime/cgo" {
		return "-import_runtime_cgo=false", "-import_syscall=false"
	}

	return "-import_runtime_cgo=true", "-import_syscall=true"
}

func (e *EmitContext) emitGoCgoCopyStmt(srcRel STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	module := instance.Path.rel()
	srcVFS := resolveSourceVFS(ctx, instance, srcRel.string(), d.srcDirs)
	dstVFS := build(module, "/", srcRel.string())
	ref := ctx.emit.reserve()

	parsed := e.scanner.parsers.sourceParsedBuckets(srcVFS, nil)
	leaves := append([]VFS{srcVFS}, ctx.scripts[copyFsToolsVFS]...)
	cgoContext := make([]VFS, 0, 2*len(d.cgoSrcs)+2)

	if len(d.cgoSrcs) > 0 {
		cgoContext = append(cgoContext, cgo1WrapperVFS)

		for _, f := range d.cgoSrcs {
			cgoContext = append(cgoContext, resolveSourceVFS(ctx, instance, f.string(), d.srcDirs))
		}

		leaves = append(leaves, cgoContext...)
		leaves = append(leaves, build(module, "/_cgo_export.h"))

		for _, f := range d.cgoSrcs {
			leaves = append(leaves, build(module, "/", strings.TrimSuffix(f.string(), ".go"), ".cgo1.go"))
		}
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     dstVFS,
		SourcePath:     srcVFS,
		ProducerRef:    ref,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed[parsedIncludesLocal]},
		ClosureLeaves:  leaves,
		Compile:        &CompileSpec{CFlags: goCgoCFlags(d)},
	})

	e.deferPass2(func() {
		cv := walkClosure(e.scanner, srcVFS, d.cc.ScanCfg)
		inputs := append([]VFS{}, ctx.scripts[copyFsToolsVFS]...)

		cv.each(func(p VFS) {
			if p.isSource() {
				inputs = append(inputs, p)
			}
		})

		inputs = append(inputs, cgoContext...)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
		args := na.strList(
			wrapccPython3STR,
			copyFsToolsVFS.str(),
			internStr("copy"),
			srcVFS.str(),
			dstVFS.str(),
		)

		node := Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args), Env: env}),
			Env:          env,
			Inputs:       na.inputList(inputs),
			KV:           &cpKV,
			Outputs:      na.vfsList(dstVFS),
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		}

		ctx.emit.emitReservedNode(node, ref)
	})
}

func (e *EmitContext) emitGoCgo1Stmt() {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.rel()

	type cgoFile struct {
		src   VFS
		cgo1  VFS
		cgo2C VFS
	}

	files := make([]cgoFile, 0, len(d.cgoSrcs))

	for _, f := range d.cgoSrcs {
		base := strings.TrimSuffix(f.string(), ".go")

		files = append(files, cgoFile{
			src:   resolveSourceVFS(ctx, instance, f.string(), d.srcDirs),
			cgo1:  build(dir, "/", base, ".cgo1.go"),
			cgo2C: build(dir, "/", base, ".cgo2.c"),
		})
	}

	exportH := build(dir, "/_cgo_export.h")
	exportC := build(dir, "/_cgo_export.c")
	gotypes := build(dir, "/_cgo_gotypes.go")
	mainC := build(dir, "/_cgo_main.c")

	args := []STR{
		wrapccPython3STR,
		cgo1WrapperVFS.str(),
		internStr("--build-prefix=/-B"),
		internStr("--source-prefix=/-S"),
		internStr("--build-root"), strB,
		internStr("--source-root"), strS,
		internStr("--cgo1-files"),
	}

	for _, f := range files {
		args = append(args, f.cgo1.str())
	}

	args = append(args, internStr("--cgo2-files"))

	for _, f := range files {
		args = append(args, f.cgo2C.str())
	}

	impRuntimeCgo, impSyscall := goCgoImportPathFlags(dir)

	args = append(args,
		internStr("--"),
		internV(resourcePatternRef("GO_TOOLS"), "/pkg/tool/linux_amd64/cgo").str(),
		internStr("-objdir"), build(dir).str(),
		internStr("-importpath"), internStr(goImportPathFor(dir)),
		internStr(impRuntimeCgo),
		internStr(impSyscall),
		internStr("--"),
		instance.Platform.TargetArg,
	)

	args = append(args, instance.Platform.SysrootArgs...)
	args = append(args, e.goCgoIncludeArgs()...)
	args = appendArgStr(args, goCgoCFlags(d))

	inputs := make([]VFS, 0, len(files)+1)

	inputs = append(inputs, cgo1WrapperVFS)

	deduper.reset()

	for _, f := range files {
		args = append(args, f.src.str())

		if deduper.add(f.src.strID()) {
			inputs = append(inputs, f.src)
		}
	}

	for _, f := range files {
		cv := walkClosure(e.scanner, f.src, d.cc.ScanCfg)

		cv.each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				inputs = append(inputs, p)
			}
		})
	}

	env := goCmdEnv(instance.Platform, d.tc)
	outputs := make([]VFS, 0, len(files)*2+4)

	for _, f := range files {
		outputs = append(outputs, f.cgo1)
	}

	for _, f := range files {
		outputs = append(outputs, f.cgo2C)
	}

	outputs = append(outputs, exportH, exportC, gotypes, mainC)

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args), Env: env, Cwd: strS}),
		Env:          env,
		Inputs:       na.inputList(inputs),
		KV:           &goToolKV,
		Outputs:      na.vfsList(outputs...),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    goToolResources(e.peers.ResourceGlobals),
	}

	ref := ctx.emit.emitNode(node)
	cgoLeaves := make([]VFS, 0, 2*len(files)+8)

	cgoLeaves = append(cgoLeaves, cgo1WrapperVFS)

	deduper.reset()

	for _, f := range files {
		if deduper.add(f.src.strID()) {
			cgoLeaves = append(cgoLeaves, f.src)
		}

		cv := walkClosure(e.scanner, f.src, d.cc.ScanCfg)

		cv.each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				cgoLeaves = append(cgoLeaves, p)
			}
		})
	}

	cgoLeaves = append(cgoLeaves, files[0].cgo1)

	cgo2Spec := &CompileSpec{CFlags: append(goCgoCFlags(d), argWnoUnusedVariable)}

	for _, f := range files {
		e.codegen.register(&GeneratedFileInfo{OutputPath: f.cgo1, ProducerRef: ref})
		e.codegen.register(&GeneratedFileInfo{OutputPath: f.cgo2C, ProducerRef: ref, Compile: cgo2Spec, ClosureLeaves: cgoLeaves})
		e.enqueueSrc(SrcMeta{Source: internStr(strings.TrimPrefix(f.cgo1.rel(), dir+"/")), Prio: stmtPrioDefault, Generated: true})
		e.enqueueSrc(SrcMeta{Source: internStr(strings.TrimPrefix(f.cgo2C.rel(), dir+"/")), Prio: stmtPrioDefault, Generated: true})
	}

	e.codegen.register(&GeneratedFileInfo{OutputPath: exportH, ProducerRef: ref})
	e.codegen.register(&GeneratedFileInfo{OutputPath: exportC, ProducerRef: ref, ClosureLeaves: cgoLeaves})
	e.codegen.register(&GeneratedFileInfo{OutputPath: gotypes, ProducerRef: ref})
	e.codegen.register(&GeneratedFileInfo{OutputPath: mainC, ProducerRef: ref})

	e.enqueueSrc(SrcMeta{Source: internStr("_cgo_export.c"), Prio: stmtPrioDefault, Generated: true})
	e.enqueueSrc(SrcMeta{Source: internStr("_cgo_gotypes.go"), Prio: stmtPrioDefault, Generated: true})
}

func (e *EmitContext) goCgoLinkOFlags() []STR {
	p := e.instance.Platform
	tc := e.d.tc

	out := make([]STR, 0, 12)

	if p.CompressDebugSections {
		out = append(out, argWlCompressDebugSectionsZstd.str())
	}

	out = append(out, p.LinkPreludeExtra...)
	out = append(out, argWlNoAsNeeded.str())

	if p.PIC {
		out = append(out, argFPIC.str(), argFPIC.str())
	}

	out = append(out,
		internStr("-fuse-ld=lld"),
		internV("--ld-path=", tc.LLD.string()),
		internStr("-Wl,--no-rosegment"),
		internStr("-Wl,--build-id=sha1"),
		internStr("-Wl,--unresolved-symbols=ignore-all"),
		internStr("-nodefaultlibs"),
		internStr("-lc"),
	)

	return out
}

func (e *EmitContext) flushGoCgo2() {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(d.cgoSrcs) == 0 {
		return
	}

	na := ctx.na
	dir := instance.Path.rel()
	mainC := build(dir, "/_cgo_main.c")
	mainO := build(dir, "/_cgo_main.c", instance.Platform.objectSuffix())
	cgoO := build(dir, "/_cgo_", instance.Platform.objectSuffix())
	importGo := build(dir, "/_cgo_import.go")
	exportH := build(dir, "/_cgo_export.h")

	objByPath := map[VFS]NodeRef{}

	for i, out := range e.outs {
		objByPath[out] = e.refs[i]
	}

	objSuf := instance.Platform.objectSuffix()
	objOrder := make([]VFS, 0, 16)

	objOrder = append(objOrder, build(dir, "/_cgo_export.c", objSuf))

	for _, f := range d.cgoSrcs {
		objOrder = append(objOrder, build(dir, "/", strings.TrimSuffix(f.string(), ".go"), ".cgo2.c", objSuf))
	}

	for _, f := range goModuleCgoCFiles(d) {
		objOrder = append(objOrder, build(dir, "/", f.string(), objSuf))
	}

	for _, f := range goModuleCgoSFiles(d) {
		objOrder = append(objOrder, build(dir, "/", f.string(), ".o"))
	}

	inclArgs := e.goCgoIncludeArgs()
	cgoFlags := goCgoCFlags(d)
	p := instance.Platform

	cmd0Args := []STR{d.tc.CC.str(), p.TargetArg}

	cmd0Args = append(cmd0Args, p.SysrootArgs...)
	cmd0Args = append(cmd0Args, inclArgs...)
	cmd0Args = appendArgStr(cmd0Args, cgoFlags)
	cmd0Args = append(cmd0Args, mainC.str(), internStr("-c"), internStr("-o"), mainO.str())

	cmd1Args := []STR{wrapccPython3STR, linkOScriptVFS.str(), d.tc.CC.str(), p.TargetArg}

	cmd1Args = append(cmd1Args, p.SysrootArgs...)
	cmd1Args = append(cmd1Args, inclArgs...)
	cmd1Args = append(cmd1Args, internStr("-o"), cgoO.str())
	cmd1Args = append(cmd1Args, e.goCgoLinkOFlags()...)
	cmd1Args = append(cmd1Args, mainO.str())

	deps := make([]NodeRef, 0, len(objOrder)+1)
	inputs := make([]VFS, 0, len(objOrder)+8)

	inputs = append(inputs, ctx.scripts[linkOScriptVFS]...)
	inputs = append(inputs, cgo1WrapperVFS, wrapccPyVFS)

	cFiles := goModuleCgoCFiles(d)

	if len(cFiles) > 0 {
		inputs = append(inputs, ctx.scripts[copyFsToolsVFS]...)
	}

	inputs = append(inputs, exportH, mainC)

	deduper.reset()

	for _, p := range inputs {
		deduper.add(p.strID())
	}

	var cgoClosureExtras []VFS

	for _, f := range d.cgoSrcs {
		src := resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)

		if deduper.add(src.strID()) {
			inputs = append(inputs, src)
		}

		cv := walkClosure(e.scanner, src, d.cc.ScanCfg)

		cv.each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				cgoClosureExtras = append(cgoClosureExtras, p)
			}
		})
	}

	inputs = append(inputs, cgoClosureExtras...)
	inputs = append(inputs, build(dir, "/", strings.TrimSuffix(d.cgoSrcs[0].string(), ".go"), ".cgo1.go"))

	for _, f := range append(cFiles, goModuleCgoSFiles(d)...) {
		src := resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)
		cv := walkClosure(e.scanner, src, d.cc.ScanCfg)

		if deduper.add(src.strID()) {
			inputs = append(inputs, src)
		}

		cv.each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				inputs = append(inputs, p)
			}
		})
	}

	for _, o := range objOrder {
		cmd1Args = append(cmd1Args, o.str())
		inputs = append(inputs, o)

		if ref, ok := objByPath[o]; ok {
			deps = append(deps, ref)
		}
	}

	for _, f := range d.cgoLdflags {
		cmd1Args = append(cmd1Args, f)
	}

	cmd2Args := na.strList(
		internV(resourcePatternRef("GO_TOOLS"), "/pkg/tool/linux_amd64/cgo").str(),
		internStr("-dynpackage"), internStr(dir[strings.LastIndexByte(dir, '/')+1:]),
		internStr("-dynimport"), cgoO.str(),
		internStr("-dynout"), importGo.str(),
		internStr("-dynlinker"),
	)

	plainEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	goEnv := goCmdEnv(p, d.tc)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(cmd0Args), Env: plainEnv},
			Cmd{CmdArgs: na.chunkList(cmd1Args), Env: plainEnv},
			Cmd{CmdArgs: na.chunkList(cmd2Args), Env: goEnv},
		),
		Env:          goEnv,
		Inputs:       na.inputList(inputs),
		KV:           &goToolKV,
		Outputs:      na.vfsList(mainO, cgoO, importGo),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      resolveCodegenDepRefsIncl(ctx, instance, na, []VFS{mainC, exportH}, deps...),
		Resources:    goToolResources(e.peers.ResourceGlobals),
	}

	ref := ctx.emit.emitNode(node)

	e.codegen.register(&GeneratedFileInfo{OutputPath: importGo, ProducerRef: ref})

	if e.goRes == nil {
		e.goRes = &GoSrcsResult{}
	}

	e.goRes.GoFiles = append(e.goRes.GoFiles, importGo)
}
