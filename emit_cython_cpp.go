package main

var cythonNumpyAddIncl = []VFS{
	intern("$(S)/contrib/python/numpy/include/numpy/core/include"),
	intern("$(S)/contrib/python/numpy/include/numpy/core/include/numpy"),
	intern("$(S)/contrib/python/numpy/include/numpy/core/src/common"),
	intern("$(S)/contrib/python/numpy/include/numpy/core/src/npymath"),
	intern("$(S)/contrib/python/numpy/include/numpy/distutils/include"),
}

var py3CythonOutputIncludes = []VFS{
	intern("$(S)/contrib/tools/cython/generated_c_headers.h"),
	intern("$(S)/contrib/tools/cython/generated_cpp_headers.h"),
	intern("$(S)/contrib/libs/python/Include/compile.h"),
	intern("$(S)/contrib/libs/python/Include/frameobject.h"),
	intern("$(S)/contrib/libs/python/Include/longintrepr.h"),
	intern("$(S)/contrib/libs/python/Include/pyconfig.h"),
	intern("$(S)/contrib/libs/python/Include/Python.h"),
	intern("$(S)/contrib/libs/python/Include/pythread.h"),
	intern("$(S)/contrib/libs/python/Include/structmember.h"),
	intern("$(S)/contrib/libs/python/Include/traceback.h"),
	intern("$(S)/contrib/libs/cxxsupp/openmp/omp.h"),
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

var cythonConstHead = []STR{
	argSContribToolsCythonCythonPy.str(),
	argX2.str(),
	argLegacyImplicitNoexceptTrue.str(),
	argE.str(),
	argUnameSysnameLinux.str(),
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

func cythonNoExt(src string) string {
	dot := -1

	for i := len(src) - 1; i >= 0; i-- {
		if src[i] == '/' {
			break
		}

		if src[i] == '.' {
			dot = i

			break
		}
	}

	if dot < 0 {
		return src
	}

	return src[:dot]
}

type cythonStmtPlan struct {
	stmt              *CythonStmt
	generatedExplicit bool
	py23Variant       bool
	generated         string
	generatedVFS      VFS
	headerVFS         []VFS
	srcVFS            VFS
	srcScanIn         ModuleCCInputs
	cyRef             NodeRef
	headerPyxClosure  []VFS
	ind               cythonCppInduced
}

func emitCythonCpp(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []*SourceEmit {
	return emitCythonCppPlanned(ctx, instance, d, in, planCythonCpp(ctx, instance, d, in))
}

func planCythonCpp(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []cythonStmtPlan {
	if len(d.cythonCpp) == 0 {
		return nil
	}

	plans := make([]cythonStmtPlan, 0, len(d.cythonCpp))

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

			generated = cythonNoExt(stmt.Src) + ext
		}

		if generatedExplicit {
			generated = *stmt.Generated
		}

		generatedVFS := build(instance.Path.rel(), "/", generated)

		var headerVFS []VFS

		if stmt.Header {
			base := instance.Path.rel() + "/" + cythonNoExt(stmt.Src)

			headerVFS = append(headerVFS, build(base, ".h"))

			if stmt.ApiHeader {
				headerVFS = append(headerVFS, build(base, "_api.h"))
			}
		}

		srcVFS := source(instance.Path.rel(), "/", stmt.Src)
		srcScanIn := in

		srcScanIn.AddIncl = appendCythonScanAddIncl(srcScanIn.AddIncl, d.cythonAddIncl, py23Variant)
		srcScanIn.ScanCfg = newScanContext(ctx.parsers, srcScanIn.AddIncl, srcScanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

		ind := cythonCppInducedSets(ctx, instance, srcVFS, stmt.CMode, srcScanIn)
		cyRef := ctx.emit.reserve()

		var headerPyxClosure []VFS

		if stmt.Header {
			headerPyxClosure = cythonPyxLangClosure(ctx.scannerFor(instance), srcVFS, srcScanIn.ScanCfg)

			pyxInduced := keepOnlySourceVFS(headerPyxClosure)
			headerInduced := cythonHeaderInducedClosure(ind)
			headerParsed := make([]IncludeDirective, 0, len(headerInduced))

			for _, v := range headerInduced {
				headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
			}

			reg := ctx.codegenFor(instance)

			for _, h := range headerVFS {
				reg.register(&GeneratedFileInfo{
					ProducerKvP:     pkCY,
					OutputPath:      h,
					ProducerRef:     cyRef,
					ParsedIncludes:  headerParsed,
					ProducerMainOut: generatedVFS,
				})

				for _, p := range pyxInduced {
					reg.addClosureLeafNoSubsume(h, p)
				}
			}
		}

		plans = append(plans, cythonStmtPlan{
			stmt:              stmt,
			generatedExplicit: generatedExplicit,
			py23Variant:       py23Variant,
			generated:         generated,
			generatedVFS:      generatedVFS,
			headerVFS:         headerVFS,
			srcVFS:            srcVFS,
			srcScanIn:         srcScanIn,
			cyRef:             cyRef,
			headerPyxClosure:  headerPyxClosure,
			ind:               ind,
		})
	}

	return plans
}

func emitCythonCppPlanned(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs, plans []cythonStmtPlan) []*SourceEmit {
	na := ctx.na

	if len(plans) == 0 {
		return nil
	}

	out := make([]*SourceEmit, 0, len(plans))

	for i := range plans {
		p := &plans[i]
		stmt := p.stmt
		generatedExplicit := p.generatedExplicit
		py23Variant := p.py23Variant
		generated := p.generated
		generatedVFS := p.generatedVFS
		headerVFS := p.headerVFS
		srcVFS := p.srcVFS
		srcScanIn := p.srcScanIn
		cyRef := p.cyRef
		sourceClosure := walkClosureTail(ctx.scannerFor(instance), srcVFS, srcScanIn.ScanCfg)
		toolInputs, emitsIncludes := cythonGeneratedOutputInputs(p.ind, sourceClosure)

		if stmt.Header {
			toolInputs = cythonHeaderToolInputs(srcVFS, p.headerPyxClosure)
		}

		if pxdVFS, ok := resolveCythonPxd(ctx, instance, in, stmt.Pxd); ok {
			pxdClosure := walkClosure(ctx.scannerFor(instance), pxdVFS, srcScanIn.ScanCfg)

			toolInputs = keepOnlySourceVFS(dedup(toolInputs, pxdClosure))
			emitsIncludes = dedup(emitsIncludes, pxdClosure)
		}

		parsed := make([]IncludeDirective, 0, len(emitsIncludes))

		for _, include := range emitsIncludes {
			parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: internStr(include.rel())})
		}

		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			ProducerKvP:    pkCY,
			OutputPath:     generatedVFS,
			ProducerRef:    cyRef,
			GeneratorRefs:  nil,
			ParsedIncludes: parsed,
		})

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
		cmdArgs := make([]STR, 0, 8+len(cythonConstHead)+len(stmt.Options))

		cmdArgs = append(cmdArgs, d.tc.Python3)
		cmdArgs = append(cmdArgs, cythonConstHead...)
		cmdArgs = appendInternStrs(cmdArgs, stmt.Options)

		if !stmt.CMode {
			cmdArgs = append(cmdArgs, argCplus.str())
		}

		cmdArgs = append(cmdArgs,
			argIB.str(),
			argIS.str(),
		)
		cmdArgs = appendCythonAddIncl(cmdArgs, d.cythonAddIncl, ctx.inclArgs)
		cmdArgs = append(cmdArgs,
			argISContribToolsCythonCythonIncludes.str(),
			(srcVFS).str(),
			argDashO.str(),
			(generatedVFS).str(),
		)

		ctx.emit.emitReserved(&Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
				Env: env}),
			Env:          env,
			Inputs:       na.inputList(toolInputs),
			Outputs:      na.vfsList(append([]VFS{generatedVFS}, headerVFS...)...),
			KV:           &cythonCppKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}, cyRef)

		ccIn := in

		ccIn.ExtraDepRefs = []NodeRef{cyRef}
		ccIn.Py3Suffix = !stmt.CMode && !generatedExplicit && py23Variant
		ccIn.AddIncl = appendCythonCCAddIncl(ccIn.AddIncl, d.cythonNumpyBeforeInclude)
		ccIn.CFlags = filterPyRegisterCFlags(ccIn.CFlags)

		ccIn.CCBlocks = composeCCModuleArgBlocks(na, instance.Platform, &ccIn)
		ccIn.PerSourceCFlags = append([]ARG(nil), in.PerSourceCFlags...)

		if cythonImplicitFallthrough(stmt, py23Variant) {
			ccIn.PerSourceCFlags = append(ccIn.PerSourceCFlags, argWnoImplicitFallthrough)
		}

		scanIn := ccIn

		scanIn.AddIncl = appendCythonScanAddIncl(in.AddIncl, d.cythonAddIncl, py23Variant)
		scanIn.ScanCfg = newScanContext(ctx.parsers, scanIn.AddIncl, scanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())
		ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), generatedVFS, scanIn.ScanCfg)

		ccIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, ccIn.IncludeInputs, cyRef)

		ccIn.IncludeInputs = cythonCompileInducedInputs(ctx, instance, ccIn.IncludeInputs)

		ccRef, ccOut, _ := emitCC(instance, internStr(generated), generatedVFS, ccIn, ctx.host, ctx.emit)

		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}

func cythonHeaderToolInputs(src VFS, pyxClosure []VFS) []VFS {
	singles := make([]VFS, 0, len(py3CythonEmbeddedFiles)+2)

	singles = append(singles, contribToolsCythonCythonPy, src)

	for _, rel := range py3CythonEmbeddedFiles {
		singles = append(singles, source(rel))
	}

	return keepOnlySourceVFS(dedup(singles, pyxClosure))
}

func cythonPyxLangClosure(scanner *IncludeScanner, src VFS, cfg ScanContext) []VFS {
	sc := scanner.getScanCtx(cfg, scanner.parsers.registry.registeredParserFor(src.rel()))

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
			if isCythonLangFile(ch.rel()) {
				visit(ch)
			}
		})
	}

	visit(src)

	return out
}

func isCythonLangFile(rel string) bool {
	return hasSuffix(rel, ".pyx") || hasSuffix(rel, ".pxd") || hasSuffix(rel, ".pxi") || hasSuffix(rel, ".py")
}

func cythonCompileInducedInputs(ctx *GenCtx, instance ModuleInstance, includeInputs []VFS) []VFS {
	reg := ctx.codegenFor(instance)

	var extra []VFS

	for _, v := range includeInputs {
		if !v.isBuild() {
			continue
		}

		if mainOut := reg.cythonMainOut(v); mainOut != 0 {
			extra = append(extra, mainOut)
		}
	}

	if len(extra) == 0 {
		return includeInputs
	}

	return concat(includeInputs, extra)
}

type cythonCppInduced struct {
	toolSingles  []VFS
	emitsSingles []VFS
	toolCl       [][]VFS
	emitsCl      [][]VFS
}

func cythonCppInducedSets(ctx *GenCtx, instance ModuleInstance, src VFS, cMode bool, scanIn ModuleCCInputs) cythonCppInduced {
	scanner := ctx.scannerFor(instance)
	toolSingles := []VFS{contribToolsCythonCythonPy}
	emitsSingles := []VFS{contribToolsCythonCythonPy, src}

	var toolCl, emitsCl [][]VFS

	for _, v := range py3CythonOutputIncludes {
		if v.rel() == "contrib/tools/cython/generated_cpp_headers.h" && cMode {
			continue
		}

		toolSingles = append(toolSingles, v)
		emitsSingles = append(emitsSingles, v)

		cl := walkClosureTail(scanner, v, scanIn.ScanCfg)

		toolCl = append(toolCl, cl)
		emitsCl = append(emitsCl, cl)
	}

	for _, rel := range py3CythonEmbeddedFiles {
		v := source(rel)

		toolSingles = append(toolSingles, v)
		emitsSingles = append(emitsSingles, v)

		cl := walkClosureTail(scanner, v, scanIn.ScanCfg)

		toolCl = append(toolCl, cl)
		emitsCl = append(emitsCl, cl)
	}

	toolSingles = append(toolSingles, src)

	return cythonCppInduced{toolSingles: toolSingles, emitsSingles: emitsSingles, toolCl: toolCl, emitsCl: emitsCl}
}

func cythonGeneratedOutputInputs(ind cythonCppInduced, sourceClosure []VFS) ([]VFS, []VFS) {
	return keepOnlySourceVFS(dedup(append([][]VFS{ind.toolSingles}, append(ind.toolCl, sourceClosure)...)...)),
		dedup(append([][]VFS{ind.emitsSingles}, append(ind.emitsCl, sourceClosure)...)...)
}

func cythonHeaderInducedClosure(ind cythonCppInduced) []VFS {
	hdrSingles := ind.toolSingles[:len(ind.toolSingles)-1]

	return dedup(append([][]VFS{hdrSingles}, ind.toolCl...)...)
}

func resolveCythonPxd(ctx *GenCtx, instance ModuleInstance, in ModuleCCInputs, pxdRel string) (VFS, bool) {
	if pxdRel == "" {
		return 0, false
	}

	if ctx.fs.isFile(dirKey(instance.Path.rel()), pxdRel) {
		return sourceJoined(instance.Path.rel(), pxdRel), true
	}

	for i := len(in.SrcDirs) - 1; i >= 1; i-- {
		if ctx.fs.isFile(in.SrcDirs[i], pxdRel) {
			return sourceJoined(in.SrcDirs[i].rel(), pxdRel), true
		}
	}

	if ctx.fs.isFile(srcRootVFS, pxdRel) {
		return source(pxdRel), true
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
	return !stmt.CMode && (hasSuffix(stmt.Src, ".pyx") || py23Variant)
}

func appendCythonAddIncl(cmdArgs []STR, addIncl []VFS, memo InclArgMemo) []STR {
	for _, path := range addIncl {
		cmdArgs = append(cmdArgs, memo.arg(path))
	}

	return cmdArgs
}

func appendCythonCCAddIncl(addIncl []VFS, numpyBeforeInclude bool) []VFS {
	out := make([]VFS, 0, len(addIncl)+len(cythonNumpyAddIncl))

	if numpyBeforeInclude {
		for i, path := range addIncl {
			if path == pythonIncludeDir {
				out = append(out, addIncl[:i]...)
				out = append(out, cythonNumpyAddIncl...)
				out = append(out, addIncl[i:]...)

				return out
			}
		}
	}

	out = append(out, addIncl...)
	out = append(out, cythonNumpyAddIncl...)

	return out
}

func adjustCythonCompanionSourceInputs(na *NodeArenas, p *Platform, d *ModuleData, src string, in ModuleCCInputs) ModuleCCInputs {
	if len(d.cythonCpp) == 0 {
		return in
	}

	if isHeaderSource(src) || isCodegenProducingSrc(src) {
		return in
	}

	if !hasSuffix(src, ".c") &&
		!hasSuffix(src, ".cc") &&
		!hasSuffix(src, ".cpp") &&
		!hasSuffix(src, ".cxx") {
		return in
	}

	in.AddIncl = appendCythonCCAddIncl(in.AddIncl, d.cythonNumpyBeforeInclude)
	in.CFlags = filterPyRegisterCFlags(in.CFlags)

	in.CCBlocks = composeCCModuleArgBlocks(na, p, &in)

	return in
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

func filterPyRegisterCFlags(cflags []ARG) []ARG {
	if len(cflags) == 0 {
		return cflags
	}

	out := make([]ARG, 0, len(cflags))

	for _, flag := range cflags {
		s := flag.string()

		if hasPrefix(s, "-DPyInit_") || hasPrefix(s, "-Dinit_module_") {
			continue
		}

		out = append(out, flag)
	}

	return out
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
