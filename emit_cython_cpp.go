package main

var cythonNumpyAddIncl = []VFS{
	Intern("$(S)/contrib/python/numpy/include/numpy/core/include"),
	Intern("$(S)/contrib/python/numpy/include/numpy/core/include/numpy"),
	Intern("$(S)/contrib/python/numpy/include/numpy/core/src/common"),
	Intern("$(S)/contrib/python/numpy/include/numpy/core/src/npymath"),
	Intern("$(S)/contrib/python/numpy/include/numpy/distutils/include"),
}

var py3CythonOutputIncludes = []VFS{
	Intern("$(S)/contrib/tools/cython/generated_c_headers.h"),
	Intern("$(S)/contrib/tools/cython/generated_cpp_headers.h"),
	Intern("$(S)/contrib/libs/python/Include/compile.h"),
	Intern("$(S)/contrib/libs/python/Include/frameobject.h"),
	Intern("$(S)/contrib/libs/python/Include/longintrepr.h"),
	Intern("$(S)/contrib/libs/python/Include/pyconfig.h"),
	Intern("$(S)/contrib/libs/python/Include/Python.h"),
	Intern("$(S)/contrib/libs/python/Include/pythread.h"),
	Intern("$(S)/contrib/libs/python/Include/structmember.h"),
	Intern("$(S)/contrib/libs/python/Include/traceback.h"),
	Intern("$(S)/contrib/libs/cxxsupp/openmp/omp.h"),
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

type CythonStmt struct {
	Src       string
	Generated *string
	Options   []string
	CMode     bool
}

func emitCythonCpp(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) []*sourceEmit {
	if len(d.cythonCpp) == 0 {
		return nil
	}

	out := make([]*sourceEmit, 0, len(d.cythonCpp))

	for _, stmt := range d.cythonCpp {
		generatedExplicit := stmt.Generated != nil
		py23Variant := d.moduleStmt != nil && cythonUsesPy23Variant(d.moduleStmt.Name)
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

		generatedVFS := Build(instance.Path + "/" + generated)
		srcVFS := Source(instance.Path + "/" + stmt.Src)
		srcScanIn := in
		srcScanIn.AddIncl = appendCythonScanAddIncl(srcScanIn.AddIncl, d.cythonAddIncl, py23Variant)
		sourceClosure := walkClosure(ctx, instance, srcVFS, srcScanIn)
		toolInputs, emitsIncludes := cythonGeneratedOutputInputs(ctx, instance, srcVFS, sourceClosure, stmt.CMode, srcScanIn)
		parsed := make([]includeDirective, 0, len(emitsIncludes))

		for _, include := range emitsIncludes {
			parsed = append(parsed, includeDirective{kind: includeQuoted, target: internStr(include.Rel())})
		}

		registerGeneratedParsedOutput(ctx, instance, "CY", generatedVFS, parsed, nil)

		env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

		cmdArgs := []STR{
			d.tc.Python3,
			argSContribToolsCythonCythonPy.str(),
			argX2.str(),
			argLegacyImplicitNoexceptTrue.str(),
			argE.str(),
			argUnameSysnameLinux.str(),
		}
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

		targetProps := TargetProperties{ModuleDir: instance.Path}

		if !stmt.CMode && !generatedExplicit && py23Variant {
			targetProps.ModuleTag = "py3"
		}

		cyRef := ctx.emit.Emit(withResources(&Node{
			Platform: instance.Platform,
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:              env,
			Inputs:           toolInputs,
			Outputs:          []VFS{generatedVFS},
			KV:               KV{P: pkCY, PC: pcYellow},
			Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
			TargetProperties: targetProps,
		}, resourcePatternYMakePython3))
		bindGeneratedOutput(ctx, instance, generatedVFS, cyRef)

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{cyRef}
		ccIn.Py3Suffix = !stmt.CMode && !generatedExplicit && py23Variant
		ccIn.AddIncl = appendCythonCCAddIncl(ccIn.AddIncl, d.cythonNumpyBeforeInclude)
		ccIn.CFlags = filterPyRegisterCFlags(ccIn.CFlags)
		ccIn.PerSourceCFlags = append([]ARG(nil), in.PerSourceCFlags...)

		if cythonImplicitFallthrough(stmt, py23Variant) {
			ccIn.PerSourceCFlags = append(ccIn.PerSourceCFlags, argWnoImplicitFallthrough)
		}

		scanIn := ccIn
		scanIn.AddIncl = appendCythonScanAddIncl(in.AddIncl, d.cythonAddIncl, py23Variant)
		ccIn.IncludeInputs = walkClosureWithSourceRel(ctx, instance, generatedVFS, srcVFS.Rel(), scanIn)

		ccRef, ccOut, _ := EmitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &sourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}

func cythonGeneratedOutputInputs(ctx *genCtx, instance ModuleInstance, src VFS, sourceClosure []VFS, cMode bool, scanIn ModuleCCInputs) ([]VFS, []VFS) {
	toolInputs := make([]VFS, 0, 2+len(py3CythonEmbeddedFiles)+len(py3CythonOutputIncludes)+len(sourceClosure))
	emitsIncludes := make([]VFS, 0, 1+len(py3CythonEmbeddedFiles)+len(py3CythonOutputIncludes)+len(sourceClosure))

	cythonPy := contribToolsCythonCythonPy
	toolInputs = append(toolInputs, cythonPy)
	emitsIncludes = append(emitsIncludes, cythonPy)
	emitsIncludes = append(emitsIncludes, src)

	for _, v := range py3CythonOutputIncludes {
		if v.Rel() == "contrib/tools/cython/generated_cpp_headers.h" && cMode {
			continue
		}

		toolInputs = append(toolInputs, v)
		emitsIncludes = append(emitsIncludes, v)
	}

	for _, rel := range py3CythonEmbeddedFiles {
		v := Source(rel)
		toolInputs = append(toolInputs, v)
		emitsIncludes = append(emitsIncludes, v)

		cl := walkClosure(ctx, instance, v, scanIn)
		toolInputs = append(toolInputs, cl...)
		emitsIncludes = append(emitsIncludes, cl...)
	}

	toolInputs = append(toolInputs, src)
	toolInputs = append(toolInputs, sourceClosure...)
	emitsIncludes = append(emitsIncludes, sourceClosure...)

	return dedupVFS(toolInputs), dedupVFS(emitsIncludes)
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

func appendCythonAddIncl(cmdArgs []STR, addIncl []VFS, memo inclArgMemo) []STR {
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

func adjustCythonCompanionSourceInputs(d *moduleData, src string, in ModuleCCInputs) ModuleCCInputs {
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
		s := flag.String()

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
