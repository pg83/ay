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
}

func emitCythonCpp(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []*SourceEmit {
	na := ctx.na

	if len(d.cythonCpp) == 0 {
		return nil
	}

	out := make([]*SourceEmit, 0, len(d.cythonCpp))

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

		if generatedExplicit {
			generated = *stmt.Generated
		}

		generatedVFS := build(instance.Path.rel() + "/" + generated)
		srcVFS := source(instance.Path.rel() + "/" + stmt.Src)
		srcScanIn := in
		srcScanIn.AddIncl = appendCythonScanAddIncl(srcScanIn.AddIncl, d.cythonAddIncl, py23Variant)
		srcScanIn.ScanCfg = newScanContext(ctx.parsers, srcScanIn.AddIncl, srcScanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())
		sourceClosure := walkClosureTail(ctx.scannerFor(instance), srcVFS, srcScanIn.ScanCfg)
		toolInputs, emitsIncludes := cythonGeneratedOutputInputs(ctx, instance, srcVFS, sourceClosure, stmt.CMode, srcScanIn)
		parsed := make([]IncludeDirective, 0, len(emitsIncludes))

		for _, include := range emitsIncludes {
			parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: internStr(include.rel())})
		}

		cyRef := ctx.emit.reserve()
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
			Outputs:          na.vfsList(generatedVFS),
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

		ccRef, ccOut, _ := emitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}

func cythonGeneratedOutputInputs(ctx *GenCtx, instance ModuleInstance, src VFS, sourceClosure []VFS, cMode bool, scanIn ModuleCCInputs) ([]VFS, []VFS) {
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
	return keepOnlySourceVFS(dedupVFS(append([][]VFS{toolSingles}, append(toolCl, sourceClosure)...)...)),
		dedupVFS(append([][]VFS{emitsSingles}, append(emitsCl, sourceClosure)...)...)
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
