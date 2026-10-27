package main

import (
	"path"
	"slices"
	"strings"
)

var (
	goCgoBaseCFlags    = []ANY{strW.any(), internStr("-pthread").any(), internStr("-fpic").any()}
	goCgoCopyCmdHead   = []ANY{wrapccPython3STR.any(), copyFsToolsVFS.any(), strCopy.any()}
	goCgo1ToolChunk    = []ANY{str3.any(), strGoCgoTool.any(), internStr("-objdir").any()}
	goCgoImportStd     = []ANY{internStr("-import_runtime_cgo=false").any(), internStr("-import_syscall=false").any()}
	goCgoImportDefault = []ANY{internStr("-import_runtime_cgo=true").any(), internStr("-import_syscall=true").any()}
	goCgoExportExts    = []string{".c", ".cc", ".cpp", ".cxx", ".C"}
)

var goCgo1Head = []ANY{
	wrapccPython3STR.any(),
	cgo1WrapperVFS.any(),
	internStr("--build-prefix=/-B").any(),
	internStr("--source-prefix=/-S").any(),
	strBuildRoot2.any(), strB.any(),
	strSourceRoot2.any(), strS.any(),
	internStr("--cgo1-files").any(),
}

var goCgoLinkOPostLd = []ANY{
	internStr("-Wl,--unresolved-symbols=ignore-all").any(),
	internStr("-nodefaultlibs").any(),
	internStr("-lc").any(),
}

func goModuleCgoExports(d *ModuleData) []int {
	var out []int

	export := false

	for i, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		rel := meta.Source.string()

		if rel == "CGO_EXPORT" {
			export = true

			continue
		}

		if export && slices.Contains(goCgoExportExts, path.Ext(rel)) {
			out = append(out, i)
		}

		export = false
	}

	return out
}

func goModuleCgoCFiles(d *ModuleData) []ANY {
	var out []ANY

	for _, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		if strings.HasSuffix(meta.Source.string(), ".c") {
			out = append(out, meta.Source)
		}
	}

	return out
}

func goModuleCgoSFiles(d *ModuleData) []ANY {
	var out []ANY

	for _, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		src := meta.Source

		if strings.HasSuffix(src.string(), ".S") {
			out = append(out, src)
		}
	}

	return out
}

func goModuleUsesCgoC(d *ModuleData) bool {
	return len(d.cgoSrcs) > 0 || slices.ContainsFunc(d.srcs, func(m SrcMeta) bool {
		ext := path.Ext(m.Source.string())

		return !m.Global && m.Compile.Variant == 0 && (ext == ".S" || slices.Contains(goCgoExportExts, ext))
	})
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

	dedupers.with(func(deduper *DeDuper) {
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
	})

	na.anys.commit(k)

	e.goInclJoined = block[:k:k]

	return e.goInclJoined
}

func goCgoImportPathFlags(std *GoStd, dir string) []ANY {
	if dir == std.Prefix+"/runtime/cgo" {
		return goCgoImportStd
	}

	return goCgoImportDefault
}

func (e *EmitContext) goCgoOut(f ANY, suffix string) VFS {
	module := e.instance.Path.relString()
	rel := f.string()

	if v, ok := moduleRootedVFS(module, rel); ok {
		rel = strings.TrimPrefix(v.relString(), module+"/")
	}

	return build(module, "/", strings.TrimSuffix(rel, ".go"), suffix)
}

func (e *EmitContext) emitGoCgoCopyStmt(srcRel ANY) {
	module := e.instance.Path.relString()
	dstVFS := build(module, "/", srcRel.string())

	if len(e.d.cgoSrcs) > 0 {
		e.emitGoCopyWithContext(srcRel, dstVFS, scanDomainCC, build(module, "/_cgo_export.h"))
	} else {
		e.emitGoCopyWithContext(srcRel, dstVFS, scanDomainCC)
	}
}

func (e *EmitContext) emitGoCopyWithContext(srcRel ANY, dstVFS VFS, domain ScanDomain, leaves ...VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	srcVFS := resolveSourceVFS(ctx, instance, srcRel.string(), d.srcDirs)
	ref := ctx.emit.reserve()
	parsed := na.dirs.alloc(1 + len(leaves))[:1+len(leaves)]

	parsed[0] = IncludeDirective{kind: includeQuoted, target: srcVFS.any()}

	for i, leaf := range leaves {
		parsed[i+1] = IncludeDirective{kind: includeQuoted, target: leaf.any()}
	}

	na.dirs.commit(len(parsed))

	scripts := ctx.scripts[copyFsToolsVFS.rel()]
	scanner := e.scanner
	scanCtx := d.scanCtx

	pe := func() {
		cv := scanner.scanClosure(srcVFS, scanCtx, domain)

		scanner.reportClosureDiagnostics(dstVFS, cv)

		block := na.vfs.alloc(len(scripts) + cv.len())
		k := 0

		for _, p := range scripts {
			block[k] = p
			k++
		}

		cv.each(func(p VFS) {
			block[k] = p
			k++
		})

		na.vfs.commit(k)

		args := na.anyList(srcVFS.any(), dstVFS.any())

		node := Node{
			Platform:  instance.Platform,
			Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(goCgoCopyCmdHead, args), Env: goVcsEnv}),
			Env:       goVcsEnv,
			Inputs:    NodeInputs{files: na.inputList(block[:k:k]), root: cv.self},
			KV:        &cpKV,
			Outputs:   na.vfsList(dstVFS),
			Resources: usesPython3,
		}

		e.emitReservedNode(node, ref)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     dstVFS,
		SourcePath:     srcVFS,
		ProducerRef:    ref,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		OnUse:          pending,
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
		files = append(files, cgoFile{
			src:   resolveSourceVFS(ctx, instance, f.string(), d.srcDirs),
			cgo1:  e.goCgoOut(f, ".cgo1.go"),
			cgo2C: e.goCgoOut(f, ".cgo2.c"),
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
		inputCap += e.scanner.walkClosure(f.src, d.scanCtx, scanDomainCC).len()
	}

	cvs := e.cvScratch[:0]

	for _, f := range files {
		cvs = append(cvs, e.scanner.walkClosure(f.src, d.scanCtx, scanDomainCC))
	}

	e.cvScratch = retainMaxLen(e.cvScratch, cvs)

	inputs := na.vfs.alloc(inputCap)
	ni := 0

	inputs[ni] = cgo1WrapperVFS
	ni++

	dedupers.with(func(deduper *DeDuper) {
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
	})

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
			na.anyList(build(dir).any(), strImportpath.any(), internStr(e.goCgoImportPath()).any()),
			goCgoImportPathFlags(instance.Platform.Go, dir),
			na.anyList(str3.any(), instance.Platform.TargetArg.any()),
			instance.Platform.SysrootArgs,
			inclArgs,
			cflagsStr,
			srcsBlock,
		), Env: env, Cwd: srcRootDirVFS}),
		Env:       env,
		Inputs:    NodeInputs{files: na.inputList(inputs[:ni:ni])},
		KV:        &goToolKV,
		Outputs:   outputs[:no:no],
		Resources: goToolResources(na, e.peers.ResourceGlobals),
	}

	ref := e.emitNode(node)
	leaves := na.vfsList(files[0].cgo1)
	pm := e.scanner.parsers
	nIncludes := 0

	for _, f := range files {
		nIncludes += len(pm.sourceParsedWalkable(f.src, nil))
	}

	includes := na.dirs.alloc(nIncludes)
	nd := 0

	for _, f := range files {
		nd += copy(includes[nd:], pm.sourceParsedWalkable(f.src, nil))
	}

	na.dirs.commit(nd)

	parsed := ParsedIncludeSet{parsedIncludesLocal: includes[:nd:nd]}
	cgoCF := goCgoCFlags(d)
	cgo2CF := na.anys.alloc(len(cgoCF) + 1)
	cgo2N := copy(cgo2CF, cgoCF)

	cgo2CF[cgo2N] = argWnoUnusedVariable.any()
	na.anys.commit(cgo2N + 1)

	cgo2Spec := CompileSpec{CFlags: cgo2CF[: cgo2N+1 : cgo2N+1]}

	for _, f := range files {
		e.register(GeneratedFileInfo{OutputPath: f.cgo1, ProducerRef: ref, ParsedIncludes: parsed})
		e.register(GeneratedFileInfo{OutputPath: f.cgo2C, ProducerRef: ref, ClosureLeaves: leaves, ParsedIncludes: parsed})
		e.enqueueSrc(SrcMeta{Source: f.cgo1.any(), Prio: stmtPrioDefault})
		e.enqueueSrc(SrcMeta{Source: f.cgo2C.any(), Prio: stmtPrioDefault, Compile: cgo2Spec})
	}

	e.register(GeneratedFileInfo{OutputPath: exportH, ProducerRef: ref, ClosureLeaves: leaves, ParsedIncludes: parsed})
	e.register(GeneratedFileInfo{OutputPath: exportC, ProducerRef: ref, ClosureLeaves: leaves, ParsedIncludes: parsed})
	e.register(GeneratedFileInfo{OutputPath: gotypes, ProducerRef: ref, ClosureLeaves: leaves, ParsedIncludes: parsed})
	e.register(GeneratedFileInfo{OutputPath: mainC, ProducerRef: ref, ClosureLeaves: leaves, ParsedIncludes: parsed})

	e.enqueueSrc(SrcMeta{Source: exportC.any(), Prio: stmtPrioDefault})
	e.enqueueSrc(SrcMeta{Source: gotypes.any(), Prio: stmtPrioDefault})
}

func (e *EmitContext) goCgoImportPath() string {
	if dir := e.instance.Path.relString(); goPackageName(dir, e.d) != "main" {
		return strings.TrimSuffix(e.instance.Platform.Go.importPath(dir), "/gotest")
	}

	return "main"
}

func (e *EmitContext) goCgoLinkOFlags() []ANY {
	return e.ctx.na.anyConcat(e.localLinkerFlagValue(nil).Args, e.peers.LDFlagsGlobal)
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
		pushObj(build(e.goCgoOut(f, ".cgo2.c").relString(), objSuf))
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
	inputs := na.vfs.alloc(len(linkOScripts) + 3 + ko + len(d.localLDFlags.Inputs))
	ni := copy(inputs, linkOScripts)

	ni += copy(inputs[ni:], []VFS{exportH, mainC, e.goCgoOut(d.cgoSrcs[0], ".cgo1.go")})
	ni += copy(inputs[ni:], objOrder[:ko])
	ni += copy(inputs[ni:], d.localLDFlags.Inputs)
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
		strDynpackage.any(), internStr(goPackageName(dir, d)).any(),
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
			), Env: goVcsEnv, Resources: resourceKeyUnion(e.peers.LDResources, e.d.localLDFlags.Resources)},
			Cmd{CmdArgs: na.chunkList(cmd2Args), Env: goEnv},
		),
		Env:       goEnv,
		Inputs:    NodeInputs{files: na.inputList(inputs[:ni:ni])},
		KV:        &goToolKV,
		Outputs:   na.vfsList(mainO, cgoO, importGo),
		DepRefs:   deps[:nd],
		Resources: goToolResources(na, e.peers.ResourceGlobals),
	}

	ref := e.emitNode(node)

	e.register(GeneratedFileInfo{OutputPath: importGo, ProducerRef: ref})

	e.goRes.GoFiles = append(e.goRes.GoFiles, importGo)
}
