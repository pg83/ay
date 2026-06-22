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
// (after the python3 token).
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
	// (ymake `noext`) and a companion .h output rides the CY node.
	Header bool
	// ApiHeader marks the _API_H variant: additionally emit a _api.h output.
	ApiHeader bool
	// Pxd is the module-relative `<mod-as-path>.pxd` candidate for a CYTHONIZE_PY
	// `.py` source (empty otherwise). Upstream pybuild.py passes it as the cython
	// macro's hidden `Dep` input when it resolves; the CY node then carries the
	// pxd and its cimport/extern-from source closure.
	Pxd string
}

// cythonNoExt strips the trailing source extension (.pyx/.py), mirroring
// ymake's `noext` modifier used by the _H / _API_H cython output macros.
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
// generated header outputs (with the Cython induced closure they pass through to
// consumers) BEFORE any source closure is walked. gen.go calls it before the
// ordinary SRCS are scanned, so a handwritten SRCS file that #includes a Cython
// generated header (gevent's callbacks.c → corecext.h) resolves the header to its
// producing CY node and rides the induced closure — the producer is otherwise
// emitted after the SRCS scan and would be invisible.
func planCythonCpp(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []cythonStmtPlan {
	if len(d.cythonCpp) == 0 {
		return nil
	}

	// Phase 1: reserve each statement's CY node and register its generated header
	// (.h / _api.h) outputs with their "pyx"-language closure BEFORE any source
	// closure is walked. A cython source can cdef-extern a sibling statement's
	// generated header (lxml objectify.pyx → includes/etreepublic.pxd →
	// `cdef extern from "etree_api.h"`), and PY_SRCS may list the consumer before
	// the producer. Registering all header outputs up front makes the producer's
	// pyx closure resolvable regardless of statement order, mirroring upstream
	// ymake, which builds the dep graph before the include-resolution add-iter.
	// The pyx closure follows only .pyx/.pxd/.pxi/.py and never a generated
	// header, so this pre-pass is self-consistent.
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
			// _H / _API_H variants strip the source extension (ymake `noext`).
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

		// The Cython "cpp"-induced sets (CYTHON_OUTPUT_INCLUDES + embedded utility
		// files and their closures) are needed here to register the header's
		// pass-through window, and again in phase 2 for the CY node / generated .c
		// inputs — compute the heavy closure walks once.
		ind := cythonCppInducedSets(ctx, instance, srcVFS, stmt.CMode, srcScanIn)

		cyRef := ctx.emit.reserve()

		var headerPyxClosure []VFS

		if stmt.Header {
			// _H / _API_H variants emit their generated .h as an addincl output;
			// upstream's cython processor routes the induced "cpp" closure (cdef
			// extern-from headers, CYTHON_OUTPUT_INCLUDES python headers, embedded-file
			// include closures) onto that header output for consumers, not onto the
			// producer node — unlike CYTHON_C/CYTHON_CPP, which keep it on the
			// producer. So the Header CY node carries only cython.py, the bare embedded
			// utility TEXT inputs, the source, and the source's cimport/include/pxd
			// closure.
			headerPyxClosure = cythonPyxLangClosure(ctx.scannerFor(instance), srcVFS, srcScanIn.ScanCfg)

			// Companion headers (.h/_api.h) are produced by the CY node; register
			// them so consumers that #include them resolve to this producer, and
			// record the source's "pyx"-language cimport/include/pxd closure as the
			// header's cython-induced set. Upstream's TCythonIncludeProcessor attaches
			// that resolved set to the node as EVI_InducedDeps "pyx" (action Use) with
			// PassInducedIncludesThroughFiles=true: a CYTHON consumer that cdef-externs
			// this generated header (lxml objectify.pyx → etreepublic.pxd →
			// `etree_api.h`) Uses the producer's pyx closure (etree.pyx + its
			// .pxi/.pxd) as its own cython source dependencies — but a C++ consumer of
			// the same header (the generated .c's compile) does NOT (it Uses only the
			// "cpp"/"h+cpp" Pass set). So the set rides the consuming CY node via an
			// explicit toolInputs augmentation (cythonInducedPyxClosure), not a
			// closure-window splice that would also reach the C++ compile.
			pyxInduced := keepOnlySourceVFS(headerPyxClosure)

			// A handwritten C/C++ source from the same module that #includes the
			// generated header (gevent's callbacks.c → corecext.h) receives, through
			// the header, the Cython "cpp"-induced closure (upstream
			// PassInducedIncludesThroughFiles): register that closure as the header's
			// parsed includes so the consumer's scan splices it as a cached window
			// (CYTHON_OUTPUT_INCLUDES + embedded utility closures: libcxx/numpy/python).
			// The Cython source itself is NOT a parsed directive — walking the .pyx
			// would re-pull its `cdef extern` C closure (gevent's libev), which the
			// header does not pass through. The bare "pyx"-language source closure and
			// the main generated output ride onto the consuming compile via
			// cythonCompileInducedInputs (the recorded CythonInducedPyx / CythonMainOut),
			// not as closure leaves — a leaf marks every member leafEver, which would
			// disable the scanner's window-subsumption skip for the producer's .pxd
			// files where they ARE traversed elsewhere (lxml objectify → etree.pxd).
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

	// Phase 2: every header output is now registered, so a consumer statement's
	// source closure resolves the producer's api header and rides its pyx closure.
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
		// (lxml objectify.pyx → etreepublic.pxd → `etree_api.h`) Uses that header's
		// recorded cython-induced "pyx" closure (etree.pyx + its .pxi/.pxd) as its
		// own cython source dependencies — the EVI_InducedDeps "pyx" Use passthrough.
		// This lands on the CY node only; the generated .c's C++ compile does not Use
		// it, so emitsIncludes is left untouched.
		toolInputs = cythonInducedPyxClosure(ctx, instance, sourceClosure, toolInputs)

		// Upstream pybuild.py: a CYTHONIZE_PY `.py` source whose module has a
		// resolving `<mod-as-path>.pxd` passes that pxd as the cython macro's
		// hidden `Dep` input. The pxd (and its cimport/extern-from source closure)
		// rides the CY node, and — like the .pyx source's own cimport closure —
		// the generated `.c`'s compile inputs as well (so the .py.c.pic.o consumer
		// matches the .pyx case, which already carries its pxd closure here).
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
		// The generated .cpp #includes codegen headers (e.g. proto .pb.h); like a
		// regular CC source (emit_cc.go), resolve their producers into deps — the CY
		// node alone does not cover them. Resolve deps over the un-augmented closure:
		// the cython-induced "pyx" augmentation below adds the api-header producer's
		// main output (already a dep through the api-header itself), so deps stay
		// byte-identical.
		ccIn.ExtraDepRefs = append([]NodeRef{cyRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, cyRef)...)

		// A generated cython .c/.cpp that #includes a sibling statement's generated
		// _api.h (objectify.pyx.c → etree_api.h) Uses the producer CY node's "pyx"
		// induced closure (etree.pyx + its .pxi/.pxd) and lists the producer's main
		// output (etree.c) — the upstream EVI_InducedDeps "pyx" Use passthrough
		// (PassInducedIncludesThroughFiles) plus the api-header OutTogether main. This
		// lands on the generated compile only; resolving the pyx files as regular
		// includes would re-pull the producer's cdef-extern C headers, so they ride
		// non-expanded.
		ccIn.IncludeInputs = cythonCompileInducedInputs(ctx, instance, ccIn.IncludeInputs)

		ccRef, ccOut, _ := emitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}

// cythonHeaderToolInputs computes the CY-node input closure for a Header
// (_H / _API_H) cython statement: cython.py, the bare embedded utility TEXT
// inputs, the source, and the source's cimport/include/pxd ("pyx"-language)
// closure — but NOT the induced "cpp" closure (cdef extern-from C/C++ headers,
// the CYTHON_OUTPUT_INCLUDES python headers, or the embedded-file include
// closures) that the non-Header path carries. The pyx-language closure is
// precomputed by the caller (it also rides the generated header's parsed
// includes, see cythonHeaderParsedIncludes).
func cythonHeaderToolInputs(src VFS, pyxClosure []VFS) []VFS {
	singles := make([]VFS, 0, len(py3CythonEmbeddedFiles)+2)
	singles = append(singles, contribToolsCythonCythonPy, src)

	for _, rel := range py3CythonEmbeddedFiles {
		singles = append(singles, source(rel))
	}

	return keepOnlySourceVFS(dedupVFS(singles, pyxClosure))
}

// cythonPyxLangClosure walks the transitive cimport/include/pxd closure of a
// cython source, following only cython-language files (.pyx/.pxd/.pxi/.py) and
// dropping `cdef extern from` C/C++ header targets — which upstream models as
// induced "cpp" deps, not direct includes. It reuses the scanner's cached
// per-file child resolution and builds no closure-window cache entry, so the
// context-free window cache stays untouched.
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
// "pyx"-language induced closure of every generated _H / _API_H header reached in
// the source's closure (the upstream EVI_InducedDeps "pyx" Use passthrough). The
// header itself is a $(B) member of sourceClosure (it carries no parsed includes
// of its own); its producer's pyx closure is read from the codegen registry and
// unioned in, so the consuming CY node carries the producer's etree.pyx + .pxi /
// .pxd. Returns toolInputs unchanged when no reached header records an induced set.
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
// closure with the cython-induced "pyx" Use passthrough of every generated _H /
// _API_H header reached in the compile's include closure: the producing CY node's
// "pyx" closure (etree.pyx + its .pxi/.pxd) and the producer's main generated
// output (etree.c, the api-header OutTogether main). Both ride as bare,
// non-expanded inputs — re-resolving the .pxd/.pyx through the cython parser would
// pull the producer's cdef-extern C headers, which the C++ compile must not carry.
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

// cythonCppInduced holds the Cython "cpp"-induced singles and their transitive
// closures (the CYTHON_OUTPUT_INCLUDES headers and the embedded utility files) —
// everything independent of the per-source .pyx closure. Computed once in the
// registration pre-pass and reused by node emission, so the heavy closure walks
// run a single time per statement.
type cythonCppInduced struct {
	// toolSingles is [cython.py, OUTPUT_INCLUDES…, embedded…, src]; emitsSingles is
	// [cython.py, src, OUTPUT_INCLUDES…, embedded…]. The orderings mirror the
	// historical CY-node / generated-.c input sequences (dedupVFS keeps the first
	// occurrence, so the final node input order is load-bearing).
	toolSingles  []VFS
	emitsSingles []VFS
	toolCl       [][]VFS
	emitsCl      [][]VFS
}

func cythonCppInducedSets(ctx *GenCtx, instance ModuleInstance, src VFS, cMode bool, scanIn ModuleCCInputs) cythonCppInduced {
	scanner := ctx.scannerFor(instance)

	// The bare tool/source/header files collect into one `singles` slice; each
	// include-closure rides as its own chunk. dedupVFS reads every chunk in a
	// single pass, so no kilometre-long intermediate concat is built. The dedup
	// is load-bearing, not defensive: the closures overlap massively (every
	// embedded .c and every OUTPUT_INCLUDES header pulls libcxx/python), so the
	// union really collapses — ~7.5k raw → ~1.3k on library/python/codecs.
	toolSingles := []VFS{contribToolsCythonCythonPy}
	emitsSingles := []VFS{contribToolsCythonCythonPy, src}
	var toolCl, emitsCl [][]VFS

	for _, v := range py3CythonOutputIncludes {
		if v.rel() == "contrib/tools/cython/generated_cpp_headers.h" && cMode {
			continue
		}

		toolSingles = append(toolSingles, v)
		emitsSingles = append(emitsSingles, v)

		// Upstream declares these via OUTPUT_INCLUDES (CYTHON_OUTPUT_INCLUDES,
		// ymake.core.conf) and scans each transitively, so the header's full
		// include closure rides on the CY node — not the bare header alone. The
		// contrib/libs/python/Include/longintrepr.h shim, for one, resolves its
		// #else `#include <contrib/tools/python/src/Include/longintrepr.h>` (ymake
		// ignores #ifdef) to the py2 longintrepr target reachable only through it.
		// See bugs/20260615-upstream-cython-cy-node-full-include-closure.md.
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
	// singles first (dedup keeps first occurrence), then the closure chunks and
	// the .pyx source closure — fed straight to the one-pass dedup as chunks.
	//
	// The cython transpile's own inputs are source-only: it reads the .pyx/.pxd
	// and source headers, not generated C++ ($(B)) headers — a generated header
	// reached in the closure rides its $(S) source (the proto/codegen leaf)
	// instead. keepOnlySourceVFS drops the $(B) entries (e.g. tvmauth's
	// ticket2.pb.h/tvm_keys.pb.h, reached via a C++ header that #includes them),
	// matching upstream (every CY node lists zero $(B) inputs). The generated
	// .cpp's own CC closure (emitsIncludes) keeps them.
	return keepOnlySourceVFS(dedupVFS(append([][]VFS{ind.toolSingles}, append(ind.toolCl, sourceClosure)...)...)),
		dedupVFS(append([][]VFS{ind.emitsSingles}, append(ind.emitsCl, sourceClosure)...)...)
}

// cythonHeaderInducedClosure returns the Cython "cpp"-induced closure that a
// generated _H / _API_H header passes through to any file that #includes it
// (upstream PassInducedIncludesThroughFiles): the CYTHON_OUTPUT_INCLUDES headers,
// the embedded utility files, and their transitive closures — but NOT the Cython
// source itself, whose `cdef extern` C closure must not pass through the header
// (it is resolved only at the generated .c's own compile). The source's
// pyx-language closure and the main generated output ride separately as bare
// closure leaves; see planCythonCpp.
func cythonHeaderInducedClosure(ind cythonCppInduced) []VFS {
	// toolSingles is [cython.py, OUTPUT_INCLUDES…, embedded…, src]; drop the
	// trailing src so the header window does not re-walk the .pyx (which would pull
	// the source's cdef-extern C closure). cython.py + OUTPUT_INCLUDES + embedded
	// are pure Cython infra and walk to a fixed libcxx/numpy/python/utility set.
	hdrSingles := ind.toolSingles[:len(ind.toolSingles)-1]

	return dedupVFS(append([][]VFS{hdrSingles}, ind.toolCl...)...)
}

// resolveCythonPxd resolves a CYTHONIZE_PY .py source's `<mod-as-path>.pxd`
// candidate the way upstream's unit.resolve_arc_path does (ResolveSourcePath
// against the module dir, then SRCDIRs, then arcadia root). Returns false when
// no such file exists, mirroring resolve_arc_path's empty result.
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
