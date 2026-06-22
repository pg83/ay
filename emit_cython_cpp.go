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

// cythonConstHead is the constant flag lead of every cython invocation
// (after the python token).
var cythonConstHead = []STR{
	argSContribToolsCythonCythonPy.str(),
	argX2.str(),
	argLegacyImplicitNoexceptTrue.str(),
	argE.str(),
	argUnameSysnameLinux.str(),
}

type CythonStmt struct {
	Src       string
	Generated *string
	Options   []string
	CMode     bool
	// Header marks the _H / _API_H variants: the source extension is stripped
	// (`noext`) and a companion .h output rides the CY node.
	Header bool
	// ApiHeader marks the _API_H variant: additionally emit a _api.h output.
	ApiHeader bool
	// Pxd is the module-relative `<mod-as-path>.pxd` candidate for a CYTHONIZE_PY
	// `.py` source (empty otherwise). Upstream passes it as the cython macro's
	// hidden `Dep` input when it resolves; the CY node then carries the pxd and
	// its cimport/extern-from source closure.
	Pxd string
}

// cythonNoExt strips the trailing source extension (.pyx/.py), mirroring the
// `noext` modifier used by the _H / _API_H cython output macros.
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

// cythonStmtPlan carries a cython statement's path/scan data from the
// registration pre-pass (phase 1) into the node-emission pass (phase 2).
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

// planCythonCpp is phase 1: reserve each statement's CY node and register its
// generated header outputs (with the induced closure they pass through) before
// any source closure is walked. It also runs before the ordinary SRCS scan, so a
// handwritten SRCS file that #includes a generated header resolves it to the
// producing CY node instead of missing the not-yet-emitted producer.
func planCythonCpp(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []cythonStmtPlan {
	if len(d.cythonCpp) == 0 {
		return nil
	}

	// A cython source can cdef-extern a sibling statement's generated header, and
	// PY_SRCS may list the consumer before the producer; registering all header
	// outputs up front makes the producer's pyx closure resolvable regardless of
	// statement order. The pyx closure follows only .pyx/.pxd/.pxi/.py and never a
	// generated header, so this pre-pass is self-consistent.
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
			// _H / _API_H variants strip the source extension (`noext`).
			ext := ".cpp"

			if stmt.CMode {
				ext = ".c"
			}

			generated = cythonNoExt(stmt.Src) + ext
		}

		if generatedExplicit {
			generated = *stmt.Generated
		}

		generatedVFS := build(instance.Path.rel() + "/" + generated)

		var headerVFS []VFS

		if stmt.Header {
			base := instance.Path.rel() + "/" + cythonNoExt(stmt.Src)
			headerVFS = append(headerVFS, build(base+".h"))

			if stmt.ApiHeader {
				headerVFS = append(headerVFS, build(base+"_api.h"))
			}
		}

		srcVFS := source(instance.Path.rel() + "/" + stmt.Src)
		srcScanIn := in
		srcScanIn.AddIncl = appendCythonScanAddIncl(srcScanIn.AddIncl, d.cythonAddIncl, py23Variant)
		srcScanIn.ScanCfg = newScanContext(ctx.parsers, srcScanIn.AddIncl, srcScanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

		// The "cpp"-induced sets (CYTHON_OUTPUT_INCLUDES + embedded utility files
		// and their closures) are needed here for the header's pass-through window
		// and again in phase 2 for the CY node / generated .c inputs — compute the
		// heavy closure walks once.
		ind := cythonCppInducedSets(ctx, instance, srcVFS, stmt.CMode, srcScanIn)

		cyRef := ctx.emit.reserve()

		var headerPyxClosure []VFS

		if stmt.Header {
			// The Header CY node carries only the source's pyx-language closure (not
			// the induced "cpp" closure, which rides the header output instead).
			headerPyxClosure = cythonPyxLangClosure(ctx.scannerFor(instance), srcVFS, srcScanIn.ScanCfg)

			// Record the pyx closure as the companion headers' induced "pyx" set: a
			// CYTHON consumer that cdef-externs the header Uses it as its own source
			// deps, but a C++ consumer (the generated .c's compile) does not. So it
			// rides the consuming CY node via cythonInducedPyxClosure, not a closure
			// splice that would also reach the C++ compile.
			pyxInduced := keepOnlySourceVFS(headerPyxClosure)

			// Register the "cpp"-induced closure as the header's parsed includes so a
			// handwritten consumer's scan splices it as a cached window. The cython
			// source itself is NOT parsed — walking the .pyx would re-pull its `cdef
			// extern` C closure, which the header does not pass through. The pyx
			// closure and main output ride via cythonCompileInducedInputs, not as
			// closure leaves — a leaf marks every member leafEver, disabling the
			// scanner's window-subsumption skip for the producer's .pxd files where
			// they ARE traversed elsewhere.
			headerInduced := cythonHeaderInducedClosure(ind)
			headerParsed := make([]IncludeDirective, 0, len(headerInduced))

			for _, v := range headerInduced {
				headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
			}

			reg := ctx.scannerFor(instance).codegen

			for _, h := range headerVFS {
				registerBoundGeneratedParsedOutput(ctx, instance, pkCY, h, headerParsed, cyRef, nil)
				reg.setCythonPyxInduced(h, pyxInduced, generatedVFS)
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

// emitCythonCppPlanned is phase 2: with every header output already registered by
// planCythonCpp, emit each statement's CY node and the generated .c/.cpp compile.
func emitCythonCppPlanned(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs, plans []cythonStmtPlan) []*SourceEmit {
	na := ctx.na

	if len(plans) == 0 {
		return nil
	}

	out := make([]*SourceEmit, 0, len(plans))

	// Every header output is now registered, so a consumer statement's source
	// closure resolves the producer's api header and rides its pyx closure.
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

		// A cython source that cdef-externs a sibling statement's generated header
		// Uses that header's recorded induced "pyx" closure as its own cython source
		// dependencies (the "pyx" Use passthrough). This lands on the CY node only;
		// the generated .c's C++ compile does not Use it, so emitsIncludes is
		// untouched.
		toolInputs = cythonInducedPyxClosure(ctx, instance, sourceClosure, toolInputs)

		// A CYTHONIZE_PY `.py` source with a resolving `<mod-as-path>.pxd` passes it
		// as the macro's hidden `Dep`. The pxd and its source closure ride both the
		// CY node and the generated `.c`'s compile, so the .py case matches the .pyx
		// case.
		if pxdVFS, ok := resolveCythonPxd(ctx, instance, in, stmt.Pxd); ok {
			pxdClosure := walkClosure(ctx.scannerFor(instance), pxdVFS, srcScanIn.ScanCfg)
			toolInputs = keepOnlySourceVFS(dedupVFS(toolInputs, pxdClosure))
			emitsIncludes = dedupVFS(emitsIncludes, pxdClosure)
		}

		parsed := make([]IncludeDirective, 0, len(emitsIncludes))

		for _, include := range emitsIncludes {
			parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: internStr(include.rel())})
		}

		registerBoundGeneratedParsedOutput(ctx, instance, pkCY, generatedVFS, parsed, cyRef, nil)

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

		targetProps := TargetProperties{ModuleDir: instance.Path.rel()}

		if !stmt.CMode && !generatedExplicit && py23Variant {
			targetProps.ModuleTag = tagPy3
		}

		ctx.emit.emitReserved(&Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
				Env: env}),
			Env:              env,
			Inputs:           na.inputList(toolInputs),
			Outputs:          na.vfsList(append([]VFS{generatedVFS}, headerVFS...)...),
			KV:               KV{P: pkCY, PC: pcYellow},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			TargetProperties: targetProps,
			Resources:        usesPython3,
		}, cyRef)

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{cyRef}
		ccIn.Py3Suffix = !stmt.CMode && !generatedExplicit && py23Variant
		ccIn.AddIncl = appendCythonCCAddIncl(ccIn.AddIncl, d.cythonNumpyBeforeInclude)
		ccIn.CFlags = filterPyRegisterCFlags(ccIn.CFlags)
		// AddIncl/CFlags feed the module-stable arg blocks — rebuild for this copy.
		ccIn.CCBlocks = composeCCModuleArgBlocks(na, instance.Platform, &ccIn)
		ccIn.PerSourceCFlags = append([]ARG(nil), in.PerSourceCFlags...)

		if cythonImplicitFallthrough(stmt, py23Variant) {
			ccIn.PerSourceCFlags = append(ccIn.PerSourceCFlags, argWnoImplicitFallthrough)
		}

		scanIn := ccIn
		scanIn.AddIncl = appendCythonScanAddIncl(in.AddIncl, d.cythonAddIncl, py23Variant)
		scanIn.ScanCfg = newScanContext(ctx.parsers, scanIn.AddIncl, scanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())
		ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), generatedVFS, scanIn.ScanCfg)
		// The generated .cpp #includes codegen headers (e.g. proto .pb.h); resolve
		// their producers into deps like a regular CC source. Resolve over the
		// un-augmented closure: the "pyx" augmentation below adds only the api-header
		// producer's main output (already a dep), so deps stay byte-identical.
		ccIn.ExtraDepRefs = append([]NodeRef{cyRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, cyRef)...)

		// Pass the producer's "pyx" closure + main output through any sibling _api.h
		// the generated .c/.cpp #includes (see cythonCompileInducedInputs).
		ccIn.IncludeInputs = cythonCompileInducedInputs(ctx, instance, ccIn.IncludeInputs)

		ccRef, ccOut, _ := emitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}

// cythonHeaderToolInputs computes the CY-node input closure for a Header
// (_H / _API_H) statement: cython.py, the bare embedded utility TEXT inputs, the
// source, and the source's pyx-language closure — but NOT the induced "cpp"
// closure the non-Header path carries. The pyx closure is precomputed by the
// caller.
func cythonHeaderToolInputs(src VFS, pyxClosure []VFS) []VFS {
	singles := make([]VFS, 0, len(py3CythonEmbeddedFiles)+2)
	singles = append(singles, contribToolsCythonCythonPy, src)

	for _, rel := range py3CythonEmbeddedFiles {
		singles = append(singles, source(rel))
	}

	return keepOnlySourceVFS(dedupVFS(singles, pyxClosure))
}

// cythonPyxLangClosure walks the cimport/include/pxd closure of a cython source,
// following only cython-language files (.pyx/.pxd/.pxi/.py) and dropping `cdef
// extern from` C/C++ header targets (induced "cpp" deps, not direct includes).
// It reuses the scanner's cached child resolution and builds no closure-window
// cache entry.
func cythonPyxLangClosure(scanner *IncludeScanner, src VFS, cfg ScanContext) []VFS {
	sc := scanner.newScanCtx(cfg, includeDirectiveParsers.registeredParserFor(src.rel()))

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

// cythonInducedPyxClosure augments a cython node's tool inputs with the recorded
// "pyx" induced closure of every generated _H / _API_H header reached in the
// source's closure (the "pyx" Use passthrough). The producer's pyx closure is
// read from the codegen registry and unioned in. Returns toolInputs unchanged
// when no reached header records an induced set.
func cythonInducedPyxClosure(ctx *GenCtx, instance ModuleInstance, sourceClosure, toolInputs []VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)

	var induced [][]VFS

	for _, v := range sourceClosure {
		if !v.isBuild() {
			continue
		}

		if pyx := reg.cythonPyxInduced(v); len(pyx) > 0 {
			induced = append(induced, pyx)
		}
	}

	if len(induced) == 0 {
		return toolInputs
	}

	return keepOnlySourceVFS(dedupVFS(append([][]VFS{toolInputs}, induced...)...))
}

// cythonCompileInducedInputs augments a generated cython .c/.cpp compile's input
// closure with the "pyx" Use passthrough of every generated _H / _API_H header
// reached: the producing CY node's "pyx" closure and its main generated output.
// Both ride as bare, non-expanded inputs — re-resolving the .pxd/.pyx would pull
// the producer's cdef-extern C headers, which the C++ compile must not carry.
// Returns includeInputs unchanged when no reached header records an induced set.
func cythonCompileInducedInputs(ctx *GenCtx, instance ModuleInstance, includeInputs []VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)

	var extra [][]VFS

	for _, v := range includeInputs {
		if !v.isBuild() {
			continue
		}

		pyx, mainOut := reg.cythonPyxInducedInfo(v)

		if len(pyx) == 0 {
			continue
		}

		extra = append(extra, pyx)

		if mainOut != 0 {
			extra = append(extra, []VFS{mainOut})
		}
	}

	if len(extra) == 0 {
		return includeInputs
	}

	return dedupVFS(append([][]VFS{includeInputs}, extra...)...)
}

// cythonCppInduced holds the "cpp"-induced singles and their closures (the
// CYTHON_OUTPUT_INCLUDES headers and the embedded utility files) — everything
// independent of the per-source .pyx closure. Computed once in the registration
// pre-pass and reused by node emission, so the heavy closure walks run once per
// statement.
type cythonCppInduced struct {
	// toolSingles is [cython.py, OUTPUT_INCLUDES…, embedded…, src]; emitsSingles is
	// [cython.py, src, OUTPUT_INCLUDES…, embedded…]. dedupVFS keeps the first
	// occurrence, so the final node input order is load-bearing.
	toolSingles  []VFS
	emitsSingles []VFS
	toolCl       [][]VFS
	emitsCl      [][]VFS
}

func cythonCppInducedSets(ctx *GenCtx, instance ModuleInstance, src VFS, cMode bool, scanIn ModuleCCInputs) cythonCppInduced {
	scanner := ctx.scannerFor(instance)

	// Bare files collect into `singles`; each include-closure rides as its own
	// chunk, so dedupVFS reads all chunks in one pass with no large concat. The
	// dedup is load-bearing: the closures overlap massively (every embedded .c and
	// OUTPUT_INCLUDES header pulls libcxx/python), collapsing ~7.5k raw → ~1.3k.
	toolSingles := []VFS{contribToolsCythonCythonPy}
	emitsSingles := []VFS{contribToolsCythonCythonPy, src}
	var toolCl, emitsCl [][]VFS

	for _, v := range py3CythonOutputIncludes {
		if v.rel() == "contrib/tools/cython/generated_cpp_headers.h" && cMode {
			continue
		}

		toolSingles = append(toolSingles, v)
		emitsSingles = append(emitsSingles, v)

		// CYTHON_OUTPUT_INCLUDES headers are scanned transitively, so each header's
		// full include closure rides the CY node, not the bare header. The
		// longintrepr.h shim, for one, resolves its
		// #else `#include` (the scanner ignores #ifdef) to the py2 longintrepr
		// target reachable only through it.
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
	// The transpile's inputs are source-only: a generated $(B) header reached in
	// the closure rides its $(S) source instead. keepOnlySourceVFS drops the $(B)
	// entries, matching upstream (every CY node lists zero $(B) inputs); the
	// generated .cpp's own CC closure (emitsIncludes) keeps them.
	return keepOnlySourceVFS(dedupVFS(append([][]VFS{ind.toolSingles}, append(ind.toolCl, sourceClosure)...)...)),
		dedupVFS(append([][]VFS{ind.emitsSingles}, append(ind.emitsCl, sourceClosure)...)...)
}

// cythonHeaderInducedClosure returns the "cpp"-induced closure a generated
// _H / _API_H header passes through to any file that #includes it: the
// CYTHON_OUTPUT_INCLUDES headers, the embedded utility files, and their closures
// — but NOT the cython source, whose `cdef extern` C closure must not pass
// through (it is resolved only at the generated .c's compile). The pyx closure
// and main output ride separately as bare leaves.
func cythonHeaderInducedClosure(ind cythonCppInduced) []VFS {
	// toolSingles is [cython.py, OUTPUT_INCLUDES…, embedded…, src]; drop the
	// trailing src so the header window does not re-walk the .pyx (which would pull
	// the source's cdef-extern C closure). The rest is pure cython infra walking
	// to a fixed libcxx/numpy/python/utility set.
	hdrSingles := ind.toolSingles[:len(ind.toolSingles)-1]

	return dedupVFS(append([][]VFS{hdrSingles}, ind.toolCl...)...)
}

// resolveCythonPxd resolves a CYTHONIZE_PY .py source's `<mod-as-path>.pxd`
// candidate the way upstream does: against the module dir, then SRCDIRs, then the
// source root. Returns false when no such file exists.
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
	// AddIncl/CFlags feed the module-stable arg blocks — rebuild for this copy.
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

	return dedupVFS(out)
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
