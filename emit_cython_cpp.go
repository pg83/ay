package main

import "strings"

var (
	cythonCppKV          = KV{P: pkCY, PC: pcYellow}
	cythonPy2SysIncludes = []VFS{contribToolsCythonPy2CythonIncludes}
	cythonPy3SysIncludes = []VFS{contribToolsCythonCythonIncludes}
	cythonInducedInputs  [2][2]cythonInducedInputsEntry
)

var cythonNumpyAddIncl = []VFS{
	source("contrib/python/numpy/include/numpy/core/include"),
	source("contrib/python/numpy/include/numpy/core/include/numpy"),
	source("contrib/python/numpy/include/numpy/core/src/common"),
	source("contrib/python/numpy/include/numpy/core/src/npymath"),
	source("contrib/python/numpy/include/numpy/distutils/include"),
}

var py3CythonOutputIncludes = []VFS{
	source("contrib/tools/cython/generated_c_headers.h"),
	source("contrib/tools/cython/generated_cpp_headers.h"),
	source("contrib/libs/python/Include/compile.h"),
	source("contrib/libs/python/Include/frameobject.h"),
	source("contrib/libs/python/Include/longintrepr.h"),
	source("contrib/libs/python/Include/pyconfig.h"),
	source("contrib/libs/python/Include/Python.h"),
	source("contrib/libs/python/Include/pythread.h"),
	source("contrib/libs/python/Include/structmember.h"),
	source("contrib/libs/python/Include/traceback.h"),
	source("contrib/libs/cxxsupp/openmp/omp.h"),
}

var py3CythonEmbeddedFiles = []string{
	"contrib/tools/cython/Cython/Utility/arrayarray.h",
	"contrib/tools/cython/Cython/Utility/AsyncGen.c",
	"contrib/tools/cython/Cython/Utility/Buffer.c",
	"contrib/tools/cython/Cython/Utility/Builtins.c",
	"contrib/tools/cython/Cython/Utility/CConvert.pyx",
	"contrib/tools/cython/Cython/Utility/CMath.c",
	"contrib/tools/cython/Cython/Utility/CommonStructures.c",
	"contrib/tools/cython/Cython/Utility/CommonTypes.c",
	"contrib/tools/cython/Cython/Utility/Complex.c",
	"contrib/tools/cython/Cython/Utility/Coroutine.c",
	"contrib/tools/cython/Cython/Utility/CpdefEnums.pyx",
	"contrib/tools/cython/Cython/Utility/CppConvert.pyx",
	"contrib/tools/cython/Cython/Utility/CppSupport.cpp",
	"contrib/tools/cython/Cython/Utility/CythonFunction.c",
	"contrib/tools/cython/Cython/Utility/Dataclasses.c",
	"contrib/tools/cython/Cython/Utility/Embed.c",
	"contrib/tools/cython/Cython/Utility/Exceptions.c",
	"contrib/tools/cython/Cython/Utility/ExtensionTypes.c",
	"contrib/tools/cython/Cython/Utility/FunctionArguments.c",
	"contrib/tools/cython/Cython/Utility/ImportExport.c",
	"contrib/tools/cython/Cython/Utility/MemoryView.pyx",
	"contrib/tools/cython/Cython/Utility/MemoryView_C.c",
	"contrib/tools/cython/Cython/Utility/ModuleSetupCode.c",
	"contrib/tools/cython/Cython/Utility/NumpyImportArray.c",
	"contrib/tools/cython/Cython/Utility/ObjectHandling.c",
	"contrib/tools/cython/Cython/Utility/Optimize.c",
	"contrib/tools/cython/Cython/Utility/Overflow.c",
	"contrib/tools/cython/Cython/Utility/Printing.c",
	"contrib/tools/cython/Cython/Utility/Profile.c",
	"contrib/tools/cython/Cython/Utility/StringTools.c",
	"contrib/tools/cython/Cython/Utility/TestCyUtilityLoader.pyx",
	"contrib/tools/cython/Cython/Utility/TestCythonScope.pyx",
	"contrib/tools/cython/Cython/Utility/TestUtilityLoader.c",
	"contrib/tools/cython/Cython/Utility/UFuncs_C.c",
	"contrib/tools/cython/Cython/Utility/arrayarray.h",
}

var py2CythonOutputIncludes = append([]VFS{
	source("contrib/tools/cython_py2/generated_c_headers.h"),
	source("contrib/tools/cython_py2/generated_cpp_headers.h"),
}, py3CythonOutputIncludes[2:]...)

var py2CythonEmbeddedFiles = []string{
	"contrib/tools/cython_py2/Cython/Utility/arrayarray.h",
	"contrib/tools/cython_py2/Cython/Utility/AsyncGen.c",
	"contrib/tools/cython_py2/Cython/Utility/Buffer.c",
	"contrib/tools/cython_py2/Cython/Utility/Builtins.c",
	"contrib/tools/cython_py2/Cython/Utility/CConvert.pyx",
	"contrib/tools/cython_py2/Cython/Utility/CMath.c",
	"contrib/tools/cython_py2/Cython/Utility/CommonStructures.c",
	"contrib/tools/cython_py2/Cython/Utility/CommonTypes.c",
	"contrib/tools/cython_py2/Cython/Utility/Complex.c",
	"contrib/tools/cython_py2/Cython/Utility/Coroutine.c",
	"contrib/tools/cython_py2/Cython/Utility/CpdefEnums.pyx",
	"contrib/tools/cython_py2/Cython/Utility/CppConvert.pyx",
	"contrib/tools/cython_py2/Cython/Utility/CppSupport.cpp",
	"contrib/tools/cython_py2/Cython/Utility/CythonFunction.c",
	"contrib/tools/cython_py2/Cython/Utility/Dataclasses.c",
	"contrib/tools/cython_py2/Cython/Utility/Embed.c",
	"contrib/tools/cython_py2/Cython/Utility/Exceptions.c",
	"contrib/tools/cython_py2/Cython/Utility/ExtensionTypes.c",
	"contrib/tools/cython_py2/Cython/Utility/FunctionArguments.c",
	"contrib/tools/cython_py2/Cython/Utility/ImportExport.c",
	"contrib/tools/cython_py2/Cython/Utility/MemoryView.pyx",
	"contrib/tools/cython_py2/Cython/Utility/MemoryView_C.c",
	"contrib/tools/cython_py2/Cython/Utility/ModuleSetupCode.c",
	"contrib/tools/cython_py2/Cython/Utility/NumpyImportArray.c",
	"contrib/tools/cython_py2/Cython/Utility/ObjectHandling.c",
	"contrib/tools/cython_py2/Cython/Utility/Optimize.c",
	"contrib/tools/cython_py2/Cython/Utility/Overflow.c",
	"contrib/tools/cython_py2/Cython/Utility/Printing.c",
	"contrib/tools/cython_py2/Cython/Utility/Profile.c",
	"contrib/tools/cython_py2/Cython/Utility/StringTools.c",
	"contrib/tools/cython_py2/Cython/Utility/TestCythonScope.pyx",
	"contrib/tools/cython_py2/Cython/Utility/TestCyUtilityLoader.pyx",
	"contrib/tools/cython_py2/Cython/Utility/TestUtilityLoader.c",
	"contrib/tools/cython_py2/Cython/Utility/UFuncs_C.c",
}

func cythonDefaultOptions(p *Platform) string {
	const options = "-X legacy_implicit_noexcept=True"

	switch {
	case p.windows():
		return options + " -E UNAME_SYSNAME=Windows"
	case p.OS == OSLinux:
		return options + " -E UNAME_SYSNAME=Linux"
	case p.OS == OSDarwin:
		return options + " -E UNAME_SYSNAME=Darwin"
	}

	return options
}

type CythonStmt struct {
	Lint        bool
	Src         string
	Native      *SrcMeta
	Python3     bool
	Generated   *string
	Options     []string
	InitialPath bool
	Directives  []string
	CMode       bool
	Header      bool
	ApiHeader   bool
	Pxd         string
	Seq         int
}

type CythonStmtPlan struct {
	source            ResolvedSource
	stmt              *CythonStmt
	generatedExplicit bool
	generatedVFS      VFS
	headerVFS         []VFS
	cyRef             NodeRef
	toolInputs        []VFS
	emittedInputs     []VFS
	infos             []*GeneratedFileInfo
}

func (e *EmitContext) planCythonCpp() []CythonStmtPlan {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(d.cythonCpp) == 0 {
		return nil
	}

	plans := make([]CythonStmtPlan, 0, len(d.cythonCpp))

	for _, stmt := range d.cythonCpp {
		generatedExplicit := stmt.Generated != nil
		cythonSuffix := ""

		if d.usePython2 {
			cythonSuffix = ".py2"
		}

		generated := stmt.Src + d.cc.objSufDot() + cythonSuffix + cythonOutExt(stmt.CMode)

		if stmt.Native != nil {
			generated = e.moduleSourceRel(stmt.Native.Source) + d.cc.objSufDot() + ".cpp"

			if !d.usePython3 && !stmt.Python3 {
				generated = e.moduleSourceRel(stmt.Native.Source) + d.cc.objSufDot() + ".py2.cpp"
			}
		}

		if stmt.Header {
			ext := ".cpp"

			if stmt.CMode {
				ext = ".c"
			}

			generated = relStem(stmt.Src) + cythonSuffix + ext
		}

		if generatedExplicit {
			generated = *stmt.Generated
		}

		generatedVFS := build(instance.Path.relString(), "/", generated)

		var headerVFS []VFS

		if stmt.Header {
			base := instance.Path.relString() + "/" + relStem(stmt.Src) + cythonSuffix

			headerVFS = append(headerVFS, build(base, ".h"))

			if stmt.ApiHeader {
				headerVFS = append(headerVFS, build(base, "_api.h"))
			}
		}

		usePy2 := d.usePython2 || stmt.Native != nil && !d.usePython3 && !stmt.Python3
		toolInputs, emittedInputs := cythonCppInducedInputs(stmt.CMode, usePy2)
		cyRef := ctx.emit.reserve()

		var infos []*GeneratedFileInfo

		if stmt.Header {
			headerInduced := dedup(emittedInputs)
			headerParsed := ctx.na.dirs.alloc(len(headerInduced))[:0]

			for _, v := range headerInduced {
				headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(v.rel().any())})
			}

			ctx.na.dirs.commit(len(headerParsed))
			headerParsed = headerParsed[:len(headerParsed):len(headerParsed)]

			for _, h := range headerVFS {
				infos = append(infos, e.register(GeneratedFileSpec{
					OutputPath:      h,
					ProducerRef:     cyRef,
					ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: headerParsed},
					ProducerMainOut: generatedVFS,
				}))
			}
		}

		plans = append(plans, CythonStmtPlan{
			stmt:              stmt,
			generatedExplicit: generatedExplicit,
			generatedVFS:      generatedVFS,
			headerVFS:         headerVFS,
			cyRef:             cyRef,
			toolInputs:        toolInputs,
			emittedInputs:     emittedInputs,
			infos:             infos,
		})
	}

	return plans
}

func (e *EmitContext) emitCythonCppPlanned(plans []CythonStmtPlan) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	scanCtx := d.scanCtx

	for i := range plans {
		p := &plans[i]
		stmt := p.stmt
		generatedExplicit := p.generatedExplicit
		generatedVFS := p.generatedVFS
		headerVFS := p.headerVFS
		srcVFS := p.source.withGenerated(e.codegen, stmt.Src, e.d.srcDirs)
		sourceInputs := e.scanner.cythonPyxLangClosure(srcVFS, scanCtx)
		cyRef := p.cyRef
		emitsIncludes := p.emittedInputs
		pxdVFS, pxdOK := resolveCythonPxd(ctx, instance, stmt.Pxd)

		var pxdInputs []VFS

		if pxdOK {
			pxdInputs = e.scanner.cythonPyxLangClosure(pxdVFS, scanCtx)
		}

		var induced []IncludeDirective

		for _, input := range sourceInputs {
			induced = append(induced, e.scanner.parsedBucketForInput(input, parsedIncludesCpp, nil)...)
		}

		for _, input := range pxdInputs {
			induced = append(induced, e.scanner.parsedBucketForInput(input, parsedIncludesCpp, nil)...)
		}

		parsed := na.dirs.alloc(len(emitsIncludes) + len(induced))[:0]

		for _, include := range emitsIncludes {
			parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(include.rel().any())})
		}

		parsed = append(parsed, induced...)

		na.dirs.commit(len(parsed))
		parsed = parsed[:len(parsed):len(parsed)]

		objSuf := STR(0)

		if !stmt.CMode && !generatedExplicit {
			objSuf = d.unit.ObjSuf
		}

		env := envVarsVCS

		var sourceFlags []ANY

		if stmt.Native != nil {
			sourceFlags = stmt.Native.Compile.CFlags
		}

		cmdArgs := na.anys.alloc(11 + len(d.cythonOptions) + len(stmt.Options) + len(stmt.Directives) + len(d.cythonAddIncl) + len(e.peers.CythonInclude) + len(sourceFlags))[:0]

		cmdArgs = append(cmdArgs, d.tc.Python3)

		if d.usePython2 || stmt.Native != nil && !d.usePython3 && !stmt.Python3 {
			cmdArgs = append(cmdArgs, argSContribToolsCythonPy2CythonPy.any())
		} else {
			cmdArgs = append(cmdArgs, argSContribToolsCythonCythonPy.any())
		}

		cmdArgs = append(cmdArgs, d.cythonOptions...)
		cmdArgs = appendInternAnys(cmdArgs, stmt.Options)

		if stmt.InitialPath {
			cmdArgs = appendInternAnys(cmdArgs, []string{"-X", "set_initial_path=" + e.cythonRootRelArcSrc(stmt.Src)})
		}

		cmdArgs = appendInternAnys(cmdArgs, stmt.Directives)

		if !stmt.CMode {
			cmdArgs = append(cmdArgs, argCplus.any())
		}

		cmdArgs = append(cmdArgs,
			argIB.any(),
			argIS.any(),
		)

		cythonIncludes := contribToolsCythonCythonIncludes

		if d.usePython2 || stmt.Native != nil && !d.usePython3 && !stmt.Python3 {
			cythonIncludes = contribToolsCythonPy2CythonIncludes
		}

		cmdArgs = appendCythonAddIncl(cmdArgs, dedup(d.cythonAddIncl, []VFS{cythonIncludes}), ctx.inclArgs)
		cmdArgs = appendCythonAddIncl(cmdArgs, e.peers.CythonInclude, ctx.inclArgs)

		cmdArgs = append(cmdArgs,
			srcVFS.any(),
			argDashO.any(),
			generatedVFS.any(),
		)

		cmdArgs = append(cmdArgs, sourceFlags...)

		na.anys.commit(len(cmdArgs))

		cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

		outputs := na.vfsList(append([]VFS{generatedVFS}, headerVFS...)...)
		toolInputs := p.toolInputs
		buildInputs := cythonToolInputs(na, toolInputs, sourceInputs)

		if pxdOK {
			buildInputs = na.dedupClosure(buildInputs, [][]VFS{pxdInputs})
		}

		textInputs := e.textInputSources(concat(sourceInputs, pxdInputs, toolInputs), len(sourceInputs)+len(pxdInputs))

		pe := func() {
			e.emitReservedNode(NodeSpec{Node: Node{Platform: instance.Platform,
				Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
					Env: env}),
				Env: env, Inputs: na.rootInputList(buildInputs), Outputs: outputs,
				KV:        &cythonCppKV,
				Resources: usesPython3}, input: InputSpec{text: textInputs},
			}, cyRef)
		}

		pending := e.pendingEmit(pe)

		for _, output := range outputs {
			e.scanner.inheritInducedIncludes(output, nil, sourceInputs)
		}

		e.register(GeneratedFileSpec{
			OutputPath:     generatedVFS,
			BuildInputs:    buildInputs,
			ProducerRef:    cyRef,
			GeneratorRefs:  nil,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
			OnUse:          pending,
		})

		for _, info := range p.infos {
			info.OnUse = pending
			info.BuildInputs = buildInputs
		}

		seq := p.stmt.Seq

		if seq == 0 {
			seq = e.d.nextDeclSeq()
		}

		meta := SrcMeta{Prio: stmtPrioDefault, Seq: seq}

		if stmt.Native != nil {
			meta = *stmt.Native
		}

		meta.Source = generatedVFS.any()
		meta.Compile = CompileSpec{ObjSuf: objSuf}
		e.enqueueSrc(meta)
	}
}

func (scanner *IncludeScanner) cythonPyxLangClosure(src VFS, cfg *ScanContext) []VFS {
	sc := scanner.getFileScanCtx(src, cfg, scanDomainCython, scanner.parsers.registry.defaultParser)

	defer scanner.putScanCtx(sc)

	seen := make(map[VFS]struct{})

	var out []VFS

	var visit func(v VFS)

	visit = func(v VFS) {
		if _, ok := seen[v]; ok {
			return
		}

		seen[v] = struct{}{}
		out = append(out, v)

		children := sc.resolvedChildren(v)

		scanner.reportDiagnostics(children.Diagnostics)

		for _, ch := range children.Paths {
			if isCythonLangFile(ch.relString()) {
				visit(ch)

				continue
			}
		}
	}

	visit(src)

	return out
}

func isCythonLangFile(rel string) bool {
	return strings.HasSuffix(rel, ".pyx") || strings.HasSuffix(rel, ".pxd") || strings.HasSuffix(rel, ".pxi") || strings.HasSuffix(rel, ".py")
}

func cythonCppInducedInputs(cMode, py2 bool) (toolInputs, emittedInputs []VFS) {
	script, outputIncludes, embedded := contribToolsCythonCythonPy, py3CythonOutputIncludes, py3CythonEmbeddedFiles

	if py2 {
		script, outputIncludes, embedded = contribToolsCythonPy2CythonPy, py2CythonOutputIncludes, py2CythonEmbeddedFiles
	}

	cache := &cythonInducedInputs[btoi(cMode)][btoi(py2)]

	if cache.built {
		return cache.tool, cache.emitted
	}

	toolInputs = []VFS{script}
	emittedInputs = nil

	for i, v := range outputIncludes {
		if i == 1 && cMode {
			continue
		}

		emittedInputs = append(emittedInputs, v)
	}

	for _, rel := range embedded {
		v := source(rel)

		toolInputs = append(toolInputs, v)
		emittedInputs = append(emittedInputs, v)
	}

	*cache = cythonInducedInputsEntry{tool: toolInputs, emitted: emittedInputs, built: true}

	return toolInputs, emittedInputs
}

type cythonInducedInputsEntry struct {
	tool, emitted []VFS
	built         bool
}

func btoi(b bool) int {
	if b {
		return 1
	}

	return 0
}

func cythonToolInputs(na *NodeArenas, inputs, sourceInputs []VFS) []VFS {
	return na.dedupClosure(inputs, [][]VFS{sourceInputs})
}

func resolveCythonPxd(ctx *GenCtx, instance ModuleInstance, pxdRel string) (VFS, bool) {
	if pxdRel == "" {
		return 0, false
	}

	if ctx.fs.isFile(instance.Path.rel(), pxdRel) {
		return sourceJoined(instance.Path.relString(), pxdRel), true
	}

	return 0, false
}

func appendCythonAddIncl(cmdArgs []ANY, addIncl []VFS, memo InclArgMemo) []ANY {
	for _, path := range addIncl {
		cmdArgs = append(cmdArgs, memo.arg(path).any())
	}

	return cmdArgs
}

func (e *EmitContext) cythonAdjustModuleCCBlocks() []VFS {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(d.cythonCpp) == 0 {
		return d.cc.AddIncl
	}

	cy := d.cc

	cy.AddIncl = appendCythonCCAddIncl(ctx.na, d.cc.AddIncl)

	d.cc.MainOutInducedInputs = true
	d.cc.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &cy)

	return appendCythonScanAddIncl(cy.AddIncl, d.cythonAddIncl, cythonSysIncludes(d), nil)
}

func cythonSysIncludes(d *ModuleData) []VFS {
	if len(d.cythonCpp) == 0 {
		return nil
	}

	for _, stmt := range d.cythonCpp {
		if stmt.Native != nil && !d.usePython3 && !stmt.Python3 {
			return cythonPy2SysIncludes
		}
	}

	if d.usePython2 {
		return cythonPy2SysIncludes
	}

	return cythonPy3SysIncludes
}

func cythonOutExt(cMode bool) string {
	if cMode {
		return ".c"
	}

	return ".cpp"
}

func appendCythonCCAddIncl(na *NodeArenas, addIncl []VFS) []VFS {
	out := na.vfs.alloc(len(addIncl) + len(cythonNumpyAddIncl))[:0]

	defer func() {
		na.vfs.commit(len(out))
	}()

	out = append(out, addIncl...)
	out = append(out, cythonNumpyAddIncl...)
	out = dedupInPlace(out)

	return out[:len(out):len(out)]
}

func appendCythonScanAddIncl(addIncl []VFS, cythonAddIncl []VFS, sysIncludes, peerIncludes []VFS) []VFS {
	out := make([]VFS, 0, len(addIncl)+len(cythonAddIncl)+len(sysIncludes)+len(peerIncludes)+1+len(cythonNumpyAddIncl))

	out = append(out, addIncl...)
	out = append(out, cythonAddIncl...)
	out = append(out, sysIncludes...)
	out = append(out, peerIncludes...)
	out = append(out, contribLibsCxxsuppLibcxxInclude)
	out = append(out, cythonNumpyAddIncl...)

	return dedup(out)
}

func (e *EmitContext) cythonRootRelArcSrc(src string) string {
	if rooted, ok := moduleRootedVFS(e.instance.Path.relString(), src); ok {
		return rooted.relString()
	}

	for _, dir := range e.d.srcDirs {
		if dir.isSource() && e.ctx.fs.isFile(dir.rel(), src) {
			return sourceJoinClean(dir.relString(), src).relString()
		}
	}

	if e.ctx.fs.isFile(srcRootRel, src) {
		return sourceClean(src).relString()
	}

	return src
}
