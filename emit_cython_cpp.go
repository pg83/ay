package main

import "strings"

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

var cythonConstHead = []ANY{
	argSContribToolsCythonCythonPy.any(),
	argX2.any(),
	argLegacyImplicitNoexceptTrue.any(),
	argE.any(),
	argUnameSysnameLinux.any(),
}

var cythonCppKV = KV{P: pkCY, PC: pcYellow}

type CythonStmt struct {
	Src       string
	Generated *string
	Options   []string
	CMode     bool
	Header    bool
	ApiHeader bool
	Pxd       string
}

type CythonStmtPlan struct {
	stmt              *CythonStmt
	generatedExplicit bool
	py23Variant       bool
	generated         string
	generatedVFS      VFS
	headerVFS         []VFS
	srcVFS            VFS
	cyRef             NodeRef
	sourceInputs      []VFS
	toolInputs        []VFS
	emittedInputs     []VFS
	infos             []*GeneratedFileInfo
}

func (e *EmitContext) emitCythonCpp() {
	e.emitCythonCppPlanned(e.planCythonCpp())
}

func (e *EmitContext) planCythonCpp() []CythonStmtPlan {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(d.cythonCpp) == 0 {
		return nil
	}

	plans := make([]CythonStmtPlan, 0, len(d.cythonCpp))

	for _, stmt := range d.cythonCpp {
		generatedExplicit := stmt.Generated != nil
		py23Variant := cythonUsesPy23Variant(d.moduleStmt.Name)
		generated := stmt.Src + ".cpp"

		if py23Variant {
			generated = stmt.Src + ".py3.cpp"
		}

		if stmt.CMode {
			generated = stmt.Src + ".c"
		}

		if stmt.Header {
			ext := ".cpp"

			if stmt.CMode {
				ext = ".c"
			}

			generated = relStem(stmt.Src) + ext
		}

		if generatedExplicit {
			generated = *stmt.Generated
		}

		generatedVFS := build(instance.Path.relString(), "/", generated)

		var headerVFS []VFS

		if stmt.Header {
			base := instance.Path.relString() + "/" + relStem(stmt.Src)

			headerVFS = append(headerVFS, build(base, ".h"))

			if stmt.ApiHeader {
				headerVFS = append(headerVFS, build(base, "_api.h"))
			}
		}

		srcVFS := source(instance.Path.relString(), "/", stmt.Src)
		toolInputs, emittedInputs := cythonCppInducedInputs(srcVFS, stmt.CMode)
		cyRef := ctx.emit.reserve()

		var infos []*GeneratedFileInfo

		if stmt.Header {
			headerInduced := make([]VFS, 0, len(emittedInputs)-1)

			headerInduced = append(headerInduced, emittedInputs[0])
			headerInduced = append(headerInduced, emittedInputs[2:]...)
			headerInduced = dedup(headerInduced)
			headerParsed := ctx.na.dirs.alloc(len(headerInduced))[:0]

			for _, v := range headerInduced {
				headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(v.rel().any())})
			}

			ctx.na.dirs.commit(len(headerParsed))
			headerParsed = headerParsed[:len(headerParsed):len(headerParsed)]

			for _, h := range headerVFS {
				infos = append(infos, e.register(GeneratedFileInfo{
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
			py23Variant:       py23Variant,
			generated:         generated,
			generatedVFS:      generatedVFS,
			headerVFS:         headerVFS,
			srcVFS:            srcVFS,
			cyRef:             cyRef,
			toolInputs:        toolInputs,
			emittedInputs:     emittedInputs,
			infos:             infos,
		})
	}

	for i := range plans {
		p := &plans[i]

		p.sourceInputs = e.scanner.cythonPyxLangClosure(p.srcVFS, d.scanCtx)

		for _, info := range p.infos {
			for _, input := range p.sourceInputs {
				e.codegen.addClosureLeafNoSubsume(info.OutputPath, input)
			}
		}
	}

	for i := range plans {
		p := &plans[i]

		p.sourceInputs = e.scanner.cythonPyxLangClosure(p.srcVFS, d.scanCtx)

		if !p.stmt.Header {
			for _, input := range p.toolInputs[1 : len(p.toolInputs)-1] {
				if isCythonLangFile(input.relString()) {
					p.sourceInputs = append(p.sourceInputs, e.scanner.cythonPyxLangClosure(input, d.scanCtx)...)
				}
			}
		}

		p.sourceInputs = dedup(p.sourceInputs)
	}

	return plans
}

func (e *EmitContext) emitCythonCppPlanned(plans []CythonStmtPlan) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(plans) == 0 {
		return
	}

	scanCtx := d.scanCtx

	for i := range plans {
		p := &plans[i]
		stmt := p.stmt
		generatedExplicit := p.generatedExplicit
		py23Variant := p.py23Variant
		generatedVFS := p.generatedVFS
		headerVFS := p.headerVFS
		srcVFS := p.srcVFS
		cyRef := p.cyRef
		emitsIncludes := p.emittedInputs
		pxdVFS, pxdOK := resolveCythonPxd(ctx, instance, stmt.Pxd)
		var pxdInputs []VFS

		if pxdOK {
			pxdInputs = e.scanner.cythonPyxLangClosure(pxdVFS, scanCtx)
			emitsIncludes = dedup(emitsIncludes, []VFS{pxdVFS})
		}

		parsed := na.dirs.alloc(len(emitsIncludes))[:0]

		for _, include := range emitsIncludes {
			parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(include.rel().any())})
		}

		na.dirs.commit(len(parsed))
		parsed = parsed[:len(parsed):len(parsed)]

		py3Suffix := !stmt.CMode && !generatedExplicit && py23Variant
		ccCFlags := na.anys.alloc(1)[:0]

		if cythonImplicitFallthrough(stmt, py23Variant) {
			ccCFlags = append(ccCFlags, argWnoImplicitFallthrough.any())
		}

		na.anys.commit(len(ccCFlags))
		ccCFlags = ccCFlags[:len(ccCFlags):len(ccCFlags)]

		env := envVarsVCS
		cmdArgs := na.anys.alloc(8 + len(cythonConstHead) + len(stmt.Options) + len(d.cythonAddIncl))[:0]

		cmdArgs = append(cmdArgs, d.tc.Python3.any())
		cmdArgs = append(cmdArgs, cythonConstHead...)
		cmdArgs = appendInternAnys(cmdArgs, stmt.Options)

		if !stmt.CMode {
			cmdArgs = append(cmdArgs, argCplus.any())
		}

		cmdArgs = append(cmdArgs,
			argIB.any(),
			argIS.any(),
		)

		cmdArgs = appendCythonAddIncl(cmdArgs, d.cythonAddIncl, ctx.inclArgs)

		cmdArgs = append(cmdArgs,
			argISContribToolsCythonCythonIncludes.any(),
			srcVFS.any(),
			argDashO.any(),
			generatedVFS.any(),
		)

		na.anys.commit(len(cmdArgs))

		cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

		outputs := na.vfsList(append([]VFS{generatedVFS}, headerVFS...)...)
		toolInputs := p.toolInputs
		sourceInputs := p.sourceInputs

		pe := func() {
			inputs := cythonToolInputs(na, toolInputs, sourceInputs)

			if pxdOK {
				inputs = na.dedupClosure(inputs, [][]VFS{pxdInputs})
			}

			ctx.emit.emitReservedNode(Node{
				Platform: instance.Platform,
				Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
					Env: env}),
				Env:          env,
				Inputs:       na.inputList(inputs),
				Outputs:      outputs,
				KV:           &cythonCppKV,
				Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
				Resources:    usesPython3,
			}, cyRef)
		}
		pending := e.ctx.na.pendingEmit(pe)

		e.register(GeneratedFileInfo{
			OutputPath:     generatedVFS,
			ProducerRef:    cyRef,
			GeneratorRefs:  nil,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
			OnUse:          pending,
		})

		// p.infos were registered earlier in planCythonCpp (pass1 header
		// registration), before pe existed; attach post hoc.
		for _, info := range p.infos {
			info.OnUse = pending
		}

		e.enqueueSrc(SrcMeta{
			Source: generatedVFS.any(), Prio: stmtPrioDefault, Bucket: bkCython,
			Compile: CompileSpec{Py3Suffix: py3Suffix, CFlags: ccCFlags},
		})
	}
}

func (scanner *IncludeScanner) cythonPyxLangClosure(src VFS, cfg *ScanContext) []VFS {
	sc := scanner.getScanCtx(cfg, scanDomainCython, scanner.parsers.registry.registeredParserFor(src.relString()))

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

		sc.forEachResolvedChildID(v, func(ch VFS) {
			if isCythonLangFile(ch.relString()) {
				visit(ch)

				return
			}

			if ch.isBuild() {
				for _, leaf := range scanner.codegen.closureLeaves(ch) {
					if isCythonLangFile(leaf.relString()) {
						visit(leaf)
					}
				}
			}
		})
	}

	visit(src)

	return out
}

func isCythonLangFile(rel string) bool {
	return strings.HasSuffix(rel, ".pyx") || strings.HasSuffix(rel, ".pxd") || strings.HasSuffix(rel, ".pxi") || strings.HasSuffix(rel, ".py")
}

func cythonCppInducedInputs(src VFS, cMode bool) (toolInputs, emittedInputs []VFS) {
	toolInputs = []VFS{contribToolsCythonCythonPy}
	emittedInputs = []VFS{contribToolsCythonCythonPy, src}

	for _, v := range py3CythonOutputIncludes {
		if v.relString() == "contrib/tools/cython/generated_cpp_headers.h" && cMode {
			continue
		}

		emittedInputs = append(emittedInputs, v)
	}

	for _, rel := range py3CythonEmbeddedFiles {
		v := source(rel)

		toolInputs = append(toolInputs, v)
		emittedInputs = append(emittedInputs, v)
	}

	toolInputs = append(toolInputs, src)

	return toolInputs, emittedInputs
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

func cythonUsesPy23Variant(modName TOK) bool {
	switch modName {
	case tokPy23Library, tokPy23NativeLibrary:
		return true
	}

	return false
}

func cythonImplicitFallthrough(stmt *CythonStmt, py23Variant bool) bool {
	return !stmt.CMode && (strings.HasSuffix(stmt.Src, ".pyx") || py23Variant)
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

	cy.AddIncl = appendCythonCCAddIncl(ctx.na, d.cc.AddIncl, d.cythonNumpyBeforeInclude)
	cy.CFlags = filterPyRegisterCFlags(d.cc.CFlags)
	d.cc.MainOutInducedInputs = true
	d.cc.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &cy)

	return appendCythonScanAddIncl(cy.AddIncl, d.cythonAddIncl, cythonUsesPy23Variant(d.moduleStmt.Name))
}

func appendCythonCCAddIncl(na *NodeArenas, addIncl []VFS, numpyBeforeInclude bool) []VFS {
	out := na.vfs.alloc(len(addIncl) + len(cythonNumpyAddIncl))[:0]

	defer func() {
		na.vfs.commit(len(out))
	}()

	if numpyBeforeInclude {
		for i, path := range addIncl {
			if path == pythonIncludeDir {
				out = append(out, addIncl[:i]...)
				out = append(out, cythonNumpyAddIncl...)
				out = append(out, addIncl[i:]...)

				return out[:len(out):len(out)]
			}
		}
	}

	out = append(out, addIncl...)
	out = append(out, cythonNumpyAddIncl...)

	return out[:len(out):len(out)]
}

func appendCythonScanAddIncl(addIncl []VFS, cythonAddIncl []VFS, py23 bool) []VFS {
	out := make([]VFS, 0, len(addIncl)+len(cythonAddIncl)+3+len(cythonNumpyAddIncl))

	out = append(out, addIncl...)
	out = append(out, cythonAddIncl...)

	if py23 {
		out = append(out, contribToolsCythonPy2CythonIncludes)
	}

	out = append(out, contribToolsCythonCythonIncludes)
	out = append(out, contribLibsCxxsuppLibcxxInclude)
	out = append(out, cythonNumpyAddIncl...)

	return dedup(out)
}

func filterPyRegisterCFlags(cflags []ANY) []ANY {
	if len(cflags) == 0 {
		return cflags
	}

	out := make([]ANY, 0, len(cflags))

	for _, flag := range cflags {
		s := flag.string()

		if strings.HasPrefix(s, "-DPyInit_") || strings.HasPrefix(s, "-Dinit_module_") {
			continue
		}

		out = append(out, flag)
	}

	return out
}
