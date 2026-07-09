package main

import (
	"strings"
)

var (
	goCgoBaseCFlags    = []ANY{internStr("-w").any(), internStr("-pthread").any(), internStr("-fpic").any()}
	goCgoCopyCmdHead   = []ANY{wrapccPython3STR.any(), copyFsToolsVFS.any(), internStr("copy").any()}
	goCgo1ToolChunk    = []ANY{str3.any(), strGoCgoTool.any(), internStr("-objdir").any()}
	goCgoImportStd     = []ANY{internStr("-import_runtime_cgo=false").any(), internStr("-import_syscall=false").any()}
	goCgoImportDefault = []ANY{internStr("-import_runtime_cgo=true").any(), internStr("-import_syscall=true").any()}
)

var goCgo1Head = []ANY{
	wrapccPython3STR.any(),
	cgo1WrapperVFS.any(),
	internStr("--build-prefix=/-B").any(),
	internStr("--source-prefix=/-S").any(),
	internStr("--build-root").any(), strB.any(),
	internStr("--source-root").any(), strS.any(),
	internStr("--cgo1-files").any(),
}

var goCgoLinkOPostLd = []ANY{
	internStr("-Wl,--no-rosegment").any(),
	internStr("-Wl,--build-id=sha1").any(),
	internStr("-Wl,--unresolved-symbols=ignore-all").any(),
	internStr("-nodefaultlibs").any(),
	internStr("-lc").any(),
}

func goModuleCgoCFiles(d *ModuleData) []ANY {
	var out []ANY

	for _, src := range d.srcs {
		rel := src.string()

		if strings.HasSuffix(rel, ".c") || strings.HasSuffix(rel, ".cxx") {
			out = append(out, src)
		}
	}

	return out
}

func goModuleCgoSFiles(d *ModuleData) []ANY {
	var out []ANY

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

func goCgoCFlags(d *ModuleData) []ANY {
	if len(d.cgoCflags) == 0 {
		return goCgoBaseCFlags
	}

	out := make([]ANY, 0, len(goCgoBaseCFlags)+len(d.cgoCflags))

	out = append(out, goCgoBaseCFlags...)

	for _, f := range d.cgoCflags {
		out = append(out, f)
	}

	return out
}

func (e *EmitContext) goCgoIncludeArgs() []ANY {
	if e.goInclJoined != nil {
		return e.goInclJoined
	}

	d := e.d
	na := e.ctx.na
	block := na.anys.alloc(2 + len(d.cc.AddIncl) + len(d.cc.PeerAddInclGlobal))
	k := 0
	push := func(x STR) { block[k] = x.any(); k++ }

	push(strIB)
	push(strIS)

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	for _, p := range d.cc.AddIncl {
		if deduper.add(p.strID()) {
			push(d.cc.InclArgs.arg(p))
		}
	}

	for _, p := range d.cc.PeerAddInclGlobal {
		if deduper.add(p.strID()) {
			push(d.cc.InclArgs.arg(p))
		}
	}

	na.anys.commit(k)

	e.goInclJoined = block[:k:k]

	return e.goInclJoined
}

func goCgoImportPathFlags(dir string) []ANY {
	if dir == goStdPrefix+"/runtime/cgo" {
		return goCgoImportStd
	}

	return goCgoImportDefault
}

func (e *EmitContext) emitGoCgoCopyStmt(srcRel ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	module := instance.Path.relString()
	srcVFS := resolveSourceVFS(ctx, instance, srcRel.string(), d.srcDirs)
	dstVFS := build(module, "/", srcRel.string())
	ref := ctx.emit.reserve()
	parsed := e.scanner.parsers.sourceParsedBuckets(srcVFS, nil)

	var cgoContext []VFS

	if len(d.cgoSrcs) > 0 {
		block := na.vfs.alloc(1 + len(d.cgoSrcs))

		block[0] = cgo1WrapperVFS

		for i, f := range d.cgoSrcs {
			block[i+1] = resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)
		}

		na.vfs.commit(1 + len(d.cgoSrcs))

		cgoContext = block[: 1+len(d.cgoSrcs) : 1+len(d.cgoSrcs)]
	}

	scripts := ctx.scripts[copyFsToolsVFS.rel()]
	leafCap := 1 + len(scripts) + len(cgoContext) + 1 + len(d.cgoSrcs)
	leafBlock := na.vfs.alloc(leafCap)
	nl := 0
	pushLeaf := func(p VFS) { leafBlock[nl] = p; nl++ }

	pushLeaf(srcVFS)

	for _, p := range scripts {
		pushLeaf(p)
	}

	if len(d.cgoSrcs) > 0 {
		for _, p := range cgoContext {
			pushLeaf(p)
		}

		pushLeaf(build(module, "/_cgo_export.h"))

		for _, f := range d.cgoSrcs {
			pushLeaf(build(module, "/", strings.TrimSuffix(f.string(), ".go"), ".cgo1.go"))
		}
	}

	na.vfs.commit(nl)

	e.codegen.register(GeneratedFileInfo{
		OutputPath:     dstVFS,
		SourcePath:     srcVFS,
		ProducerRef:    ref,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed[parsedIncludesLocal]},
		ClosureLeaves:  leafBlock[:nl:nl],
		Compile:        e.ctx.na.compileSpec(CompileSpec{CFlags: goCgoCFlags(d)}),
	})

	e.deferPass2(func() {
		cv := walkClosure(e.scanner, srcVFS, d.cc.ScanCfg)
		block := na.vfs.alloc(len(scripts) + cv.len() + len(cgoContext))
		k := 0

		for _, p := range scripts {
			block[k] = p
			k++
		}

		cv.each(func(p VFS) {
			if p.isSource() {
				block[k] = p
				k++
			}
		})

		for _, p := range cgoContext {
			block[k] = p
			k++
		}

		na.vfs.commit(k)

		args := na.anyList(srcVFS.any(), dstVFS.any())

		node := Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(goCgoCopyCmdHead, args), Env: goVcsEnv}),
			Env:          goVcsEnv,
			Inputs:       na.inputList(block[:k:k]),
			KV:           &cpKV,
			Outputs:      na.vfsList(dstVFS),
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}

		ctx.emit.emitReservedNode(node, ref)
	})
}

func (e *EmitContext) emitGoCgo1Stmt() {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()

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
	pathBlock := na.anys.alloc(2*len(files) + 1)
	k := 0

	for _, f := range files {
		pathBlock[k] = f.cgo1.any()
		k++
	}

	pathBlock[k] = strCgo2Files.any()
	k++

	for _, f := range files {
		pathBlock[k] = f.cgo2C.any()
		k++
	}

	na.anys.commit(k)

	srcsBlock := na.anys.alloc(len(files))[:len(files):len(files)]

	for i, f := range files {
		srcsBlock[i] = f.src.any()
	}

	na.anys.commit(len(files))

	inclArgs := e.goCgoIncludeArgs()
	cflagsStr := goCgoCFlags(e.d)
	inputCap := 1 + len(files)

	for _, f := range files {
		inputCap += walkClosure(e.scanner, f.src, d.cc.ScanCfg).len()
	}

	cvs := e.cvScratch[:0]

	for _, f := range files {
		cvs = append(cvs, walkClosure(e.scanner, f.src, d.cc.ScanCfg))
	}

	e.cvScratch = cvs

	inputs := na.vfs.alloc(inputCap)
	ni := 0

	inputs[ni] = cgo1WrapperVFS
	ni++

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	for _, f := range files {
		if deduper.add(f.src.strID()) {
			inputs[ni] = f.src
			ni++
		}
	}

	for _, cv := range cvs {
		cv.each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				inputs[ni] = p
				ni++
			}
		})
	}

	na.vfs.commit(ni)

	env := goCmdEnv(ctx, instance.Platform, d.tc)
	outputs := na.vfs.alloc(2*len(files) + 4)
	no := 0

	for _, f := range files {
		outputs[no] = f.cgo1
		no++
	}

	for _, f := range files {
		outputs[no] = f.cgo2C
		no++
	}

	outputs[no] = exportH
	outputs[no+1] = exportC
	outputs[no+2] = gotypes
	outputs[no+3] = mainC
	no += 4

	na.vfs.commit(no)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(
			goCgo1Head,
			pathBlock[:k:k],
			goCgo1ToolChunk,
			na.anyList(build(dir).any(), strImportpath.any(), internStr(goImportPathFor(dir)).any()),
			goCgoImportPathFlags(dir),
			na.anyList(str3.any(), instance.Platform.TargetArg.any()),
			instance.Platform.SysrootArgs,
			inclArgs,
			cflagsStr,
			srcsBlock,
		), Env: env, Cwd: srcRootDirVFS}),
		Env:          env,
		Inputs:       na.inputList(inputs[:ni:ni]),
		KV:           &goToolKV,
		Outputs:      outputs[:no:no],
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    goToolResources(na, e.peers.ResourceGlobals),
	}

	ref := ctx.emit.emitNode(node)
	leafCap := 2 + inputCap
	cgoLeaves := na.vfs.alloc(leafCap)
	nl := 0

	cgoLeaves[nl] = cgo1WrapperVFS
	nl++

	deduper.reset()

	for i, f := range files {
		if deduper.add(f.src.strID()) {
			cgoLeaves[nl] = f.src
			nl++
		}

		cvs[i].each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				cgoLeaves[nl] = p
				nl++
			}
		})
	}

	cgoLeaves[nl] = files[0].cgo1
	nl++

	na.vfs.commit(nl)

	leaves := cgoLeaves[:nl:nl]
	cgoCF := goCgoCFlags(d)
	cgo2CF := na.anys.alloc(len(cgoCF) + 1)
	cgo2N := copy(cgo2CF, cgoCF)

	cgo2CF[cgo2N] = argWnoUnusedVariable.any()
	na.anys.commit(cgo2N + 1)

	cgo2Spec := na.compileSpec(CompileSpec{CFlags: cgo2CF[: cgo2N+1 : cgo2N+1]})
	dirPrefix := dir + "/"

	for _, f := range files {
		e.codegen.register(GeneratedFileInfo{OutputPath: f.cgo1, ProducerRef: ref})
		e.codegen.register(GeneratedFileInfo{OutputPath: f.cgo2C, ProducerRef: ref, Compile: cgo2Spec, ClosureLeaves: leaves})
		e.enqueueSrc(SrcMeta{Source: internStr(strings.TrimPrefix(f.cgo1.relString(), dirPrefix)).any(), Prio: stmtPrioDefault, Generated: true})
		e.enqueueSrc(SrcMeta{Source: internStr(strings.TrimPrefix(f.cgo2C.relString(), dirPrefix)).any(), Prio: stmtPrioDefault, Generated: true})
	}

	e.codegen.register(GeneratedFileInfo{OutputPath: exportH, ProducerRef: ref})
	e.codegen.register(GeneratedFileInfo{OutputPath: exportC, ProducerRef: ref, ClosureLeaves: leaves})
	e.codegen.register(GeneratedFileInfo{OutputPath: gotypes, ProducerRef: ref})
	e.codegen.register(GeneratedFileInfo{OutputPath: mainC, ProducerRef: ref})

	e.enqueueSrc(SrcMeta{Source: strCgoExportC.any(), Prio: stmtPrioDefault, Generated: true})
	e.enqueueSrc(SrcMeta{Source: strCgoGotypesGo.any(), Prio: stmtPrioDefault, Generated: true})
}

func (e *EmitContext) goCgoLinkOFlags() []ANY {
	p := e.instance.Platform
	tc := e.d.tc
	na := e.ctx.na
	block := na.anys.alloc(6 + len(p.LinkPreludeExtra))
	k := 0
	push := func(x ANY) { block[k] = x; k++ }

	if p.CompressDebugSections {
		push(argWlCompressDebugSectionsZstd.any())
	}

	for _, s := range p.LinkPreludeExtra {
		push(s)
	}

	push(argWlNoAsNeeded.any())

	if p.PIC {
		push(argFPIC.any())
		push(argFPIC.any())
	}

	push(argFuseLdLld.any())
	push(internV("--ld-path=", tc.LLD.prefix(), tc.LLD.relString()).any())

	na.anys.commit(k)

	return block[:k:k]
}

func (e *EmitContext) flushGoCgo2() {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(d.cgoSrcs) == 0 {
		return
	}

	na := ctx.na
	p := instance.Platform
	dir := instance.Path.relString()
	mainC := build(dir, "/_cgo_main.c")
	mainO := build(dir, "/_cgo_main.c", p.objectSuffix())
	cgoO := build(dir, "/_cgo_", p.objectSuffix())
	importGo := build(dir, "/_cgo_import.go")
	exportH := build(dir, "/_cgo_export.h")
	objByPath := map[VFS]NodeRef{}

	for i, out := range e.outs {
		objByPath[out] = e.refs[i]
	}

	cFiles := goModuleCgoCFiles(d)
	sFiles := goModuleCgoSFiles(d)
	objSuf := p.objectSuffix()
	nObjs := 1 + len(d.cgoSrcs) + len(cFiles) + len(sFiles)
	objOrder := na.vfs.alloc(nObjs)
	ko := 0
	pushObj := func(o VFS) { objOrder[ko] = o; ko++ }

	pushObj(build(dir, "/_cgo_export.c", objSuf))

	for _, f := range d.cgoSrcs {
		pushObj(build(dir, "/", strings.TrimSuffix(f.string(), ".go"), ".cgo2.c", objSuf))
	}

	for _, f := range cFiles {
		pushObj(build(dir, "/", f.string(), objSuf))
	}

	for _, f := range sFiles {
		pushObj(build(dir, "/", f.string(), ".o"))
	}

	na.vfs.commit(ko)

	inclArgs := e.goCgoIncludeArgs()
	cflagsStr := goCgoCFlags(e.d)
	linkOFlags := e.goCgoLinkOFlags()
	linkOScripts := ctx.scripts[linkOScriptVFS.rel()]
	copyFsScripts := ctx.scripts[copyFsToolsVFS.rel()]

	if len(cFiles) == 0 {
		copyFsScripts = nil
	}

	inputCap := len(linkOScripts) + len(copyFsScripts) + 4 + 1 + nObjs
	srcMark := len(e.prodVFS)
	cvs := e.cvScratch[:0]

	for _, f := range d.cgoSrcs {
		src := resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)
		cv := walkClosure(e.scanner, src, d.cc.ScanCfg)

		e.prodVFS = append(e.prodVFS, src)
		cvs = append(cvs, cv)
		inputCap += 1 + cv.len()
	}

	for _, f := range append(cFiles, sFiles...) {
		src := resolveSourceVFS(ctx, instance, f.string(), d.srcDirs)
		cv := walkClosure(e.scanner, src, d.cc.ScanCfg)

		e.prodVFS = append(e.prodVFS, src)
		cvs = append(cvs, cv)
		inputCap += 1 + cv.len()
	}

	e.cvScratch = cvs

	resolvedSrcs := e.prodVFSTake(srcMark)
	inputs := na.vfs.alloc(inputCap)
	ni := 0
	pushIn := func(v VFS) { inputs[ni] = v; ni++ }

	for _, s := range linkOScripts {
		pushIn(s)
	}

	pushIn(cgo1WrapperVFS)
	pushIn(wrapccPyVFS)

	for _, s := range copyFsScripts {
		pushIn(s)
	}

	pushIn(exportH)
	pushIn(mainC)

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	for _, v := range inputs[:ni] {
		deduper.add(v.strID())
	}

	var cgoClosureExtras []VFS

	for i := range d.cgoSrcs {
		src := resolvedSrcs[i]

		if deduper.add(src.strID()) {
			pushIn(src)
		}

		cvs[i].each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				cgoClosureExtras = append(cgoClosureExtras, p)
			}
		})
	}

	for _, v := range cgoClosureExtras {
		pushIn(v)
	}

	pushIn(build(dir, "/", strings.TrimSuffix(d.cgoSrcs[0].string(), ".go"), ".cgo1.go"))

	for i := len(d.cgoSrcs); i < len(resolvedSrcs); i++ {
		src := resolvedSrcs[i]

		if deduper.add(src.strID()) {
			pushIn(src)
		}

		cvs[i].each(func(p VFS) {
			if p.isSource() && deduper.add(p.strID()) {
				pushIn(p)
			}
		})
	}

	for _, o := range objOrder[:ko] {
		pushIn(o)
	}

	na.vfs.commit(ni)

	objArgs := na.anys.alloc(1 + nObjs + len(d.cgoLdflags))
	kd := 0

	objArgs[kd] = mainO.any()
	kd++

	deps := na.noderefs.alloc(nObjs)
	nd := 0

	for _, o := range objOrder[:ko] {
		objArgs[kd] = o.any()
		kd++

		if ref, ok := objByPath[o]; ok {
			deps[nd] = ref
			nd++
		}
	}

	for _, f := range d.cgoLdflags {
		objArgs[kd] = f
		kd++
	}

	na.anys.commit(kd)
	na.noderefs.commit(nd)

	cmd2Args := na.anyList(
		strGoCgoTool.any(),
		strDynpackage.any(), internStr(dir[strings.LastIndexByte(dir, '/')+1:]).any(),
		strDynimport.any(), cgoO.any(),
		strDynout.any(), importGo.any(),
		strDynlinker.any(),
	)

	goEnv := goCmdEnv(ctx, p, d.tc)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(
				na.anyList(d.tc.CC.any(), p.TargetArg.any()),
				p.SysrootArgs,
				inclArgs,
				cflagsStr,
				na.anyList(mainC.any(), strC2.any(), strO.any(), mainO.any()),
			), Env: goVcsEnv},
			Cmd{CmdArgs: na.chunkList(
				na.anyList(wrapccPython3STR.any(), linkOScriptVFS.any(), d.tc.CC.any(), p.TargetArg.any()),
				p.SysrootArgs,
				inclArgs,
				na.anyList(strO.any(), cgoO.any()),
				linkOFlags,
				goCgoLinkOPostLd,
				objArgs[:kd:kd],
			), Env: goVcsEnv},
			Cmd{CmdArgs: na.chunkList(cmd2Args), Env: goEnv},
		),
		Env:          goEnv,
		Inputs:       na.inputList(inputs[:ni:ni]),
		KV:           &goToolKV,
		Outputs:      na.vfsList(mainO, cgoO, importGo),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      resolveCodegenDepRefsIncl(ctx, instance, na, []VFS{mainC, exportH}, deps[:nd]...),
		Resources:    goToolResources(na, e.peers.ResourceGlobals),
	}

	ref := ctx.emit.emitNode(node)

	e.codegen.register(GeneratedFileInfo{OutputPath: importGo, ProducerRef: ref})

	if e.goRes == nil {
		e.goRes = &GoSrcsResult{}
	}

	e.goRes.GoFiles = append(e.goRes.GoFiles, importGo)
}
