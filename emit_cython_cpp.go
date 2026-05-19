package main

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
		srcScanIn.AddIncl = appendCythonScanAddIncl(srcScanIn.AddIncl, d.cythonAddIncl)
		sourceClosure := walkClosure(ctx, instance, srcVFS, srcScanIn)
		toolInputs, emitsIncludes := cythonGeneratedOutputInputs(srcVFS, sourceClosure, stmt.CMode)
		parsed := make([]includeDirective, 0, len(emitsIncludes))
		for _, include := range emitsIncludes {
			parsed = append(parsed, includeDirective{kind: includeQuoted, target: include.Rel})
		}
		registerGeneratedParsedOutput(ctx, instance, "CY", generatedVFS, parsed)

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
		}

		cmdArgs := []string{
			instance.Platform.Tools.Python3,
			"$(S)/contrib/tools/cython/cython.py",
			"-X",
			"legacy_implicit_noexcept=True",
			"-E",
			"UNAME_SYSNAME=Linux",
		}
		cmdArgs = append(cmdArgs, stmt.Options...)
		if !stmt.CMode {
			cmdArgs = append(cmdArgs, "--cplus")
		}
		cmdArgs = append(cmdArgs,
			"-I$(B)",
			"-I$(S)",
		)
		cmdArgs = appendCythonAddIncl(cmdArgs, d.addIncl)
		cmdArgs = appendCythonAddIncl(cmdArgs, d.cythonAddIncl)
		cmdArgs = append(cmdArgs,
			"-I$(S)/contrib/tools/cython/Cython/Includes",
			srcVFS.String(),
			"-o",
			generatedVFS.String(),
		)

		targetProps := map[string]string{
			"module_dir": instance.Path,
		}
		if !stmt.CMode && !generatedExplicit && py23Variant {
			targetProps["module_tag"] = "py3"
		}

		cyRef := ctx.emit.Emit(&Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  toolInputs,
			Outputs: []VFS{generatedVFS},
			KV: map[string]string{
				"p":  "CY",
				"pc": "yellow",
			},
			Platform:     string(instance.Platform.Target),
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
			Tags:             instance.Platform.Tags,
			TargetProperties: targetProps,
		})
		bindGeneratedOutput(ctx, instance, generatedVFS, cyRef)

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{cyRef}
		ccIn.Py3Suffix = !stmt.CMode && !generatedExplicit && py23Variant
		ccIn.AddIncl = appendCythonCCAddIncl(ccIn.AddIncl)
		ccIn.CFlags = filterPyRegisterCFlags(ccIn.CFlags)
		ccIn.PerSourceCFlags = append([]string(nil), in.PerSourceCFlags...)
		if !stmt.CMode {
			ccIn.PerSourceCFlags = append(ccIn.PerSourceCFlags, "-Wno-implicit-fallthrough")
		}
		scanIn := ccIn
		scanIn.AddIncl = appendCythonScanAddIncl(in.AddIncl, d.cythonAddIncl)
		ccIn.IncludeInputs = walkClosureWithSourceRel(ctx, instance, generatedVFS, srcVFS.Rel, scanIn)

		ccRef, ccOut := EmitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		ccInputs := append([]VFS{generatedVFS}, ccIn.IncludeInputs...)

		out = append(out, &sourceEmit{
			Ref:          ccRef,
			OutPath:      ccOut,
			CcIns:        ccInputs,
			PrimaryCount: 1,
		})
	}

	return out
}

func cythonGeneratedOutputInputs(src VFS, sourceClosure []VFS, cMode bool) ([]VFS, []VFS) {
	toolInputs := make([]VFS, 0, 2+len(py3CythonEmbeddedFiles)+len(py3CythonOutputIncludes)+len(sourceClosure))
	emitsIncludes := make([]VFS, 0, 1+len(py3CythonEmbeddedFiles)+len(py3CythonOutputIncludes)+len(sourceClosure))

	cythonPy := Source("contrib/tools/cython/cython.py")
	toolInputs = append(toolInputs, cythonPy)
	emitsIncludes = append(emitsIncludes, cythonPy)
	emitsIncludes = append(emitsIncludes, src)

	for _, v := range py3CythonOutputIncludes {
		if v.Rel == "contrib/tools/cython/generated_cpp_headers.h" && cMode {
			continue
		}
		toolInputs = append(toolInputs, v)
		emitsIncludes = append(emitsIncludes, v)
	}

	for _, rel := range py3CythonEmbeddedFiles {
		v := Source(rel)
		toolInputs = append(toolInputs, v)
		emitsIncludes = append(emitsIncludes, v)
	}

	toolInputs = append(toolInputs, src)
	toolInputs = append(toolInputs, sourceClosure...)
	emitsIncludes = append(emitsIncludes, sourceClosure...)

	return dedupVFS(toolInputs), dedupVFS(emitsIncludes)
}

func cythonUsesPy23Variant(modName string) bool {
	switch modName {
	case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		return true
	}

	return false
}

func appendCythonAddIncl(cmdArgs []string, addIncl []VFS) []string {
	for _, path := range addIncl {
		cmdArgs = append(cmdArgs, includeArg(path))
	}

	return cmdArgs
}

func appendCythonCCAddIncl(addIncl []VFS) []VFS {
	out := make([]VFS, 0, len(addIncl)+len(cythonNumpyAddIncl))
	out = append(out, addIncl...)
	out = append(out, cythonNumpyAddIncl...)

	return out
}

func appendCythonScanAddIncl(addIncl []VFS, cythonAddIncl []VFS) []VFS {
	out := make([]VFS, 0, len(addIncl)+len(cythonAddIncl)+2+len(cythonNumpyAddIncl))
	out = append(out, addIncl...)
	out = append(out, cythonAddIncl...)
	out = append(out, Source("contrib/tools/cython/Cython/Includes"))
	out = append(out, Source("contrib/libs/cxxsupp/libcxx/include"))
	out = append(out, cythonNumpyAddIncl...)

	return dedupVFS(out)
}

func filterPyRegisterCFlags(cflags []string) []string {
	if len(cflags) == 0 {
		return cflags
	}

	out := make([]string, 0, len(cflags))
	for _, flag := range cflags {
		if hasPrefix(flag, "-DPyInit_") || hasPrefix(flag, "-Dinit_module_") {
			continue
		}
		out = append(out, flag)
	}

	return out
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

var cythonNumpyAddIncl = []VFS{
	Source("contrib/python/numpy/include/numpy/core/include"),
	Source("contrib/python/numpy/include/numpy/core/include/numpy"),
	Source("contrib/python/numpy/include/numpy/core/src/common"),
	Source("contrib/python/numpy/include/numpy/core/src/npymath"),
	Source("contrib/python/numpy/include/numpy/distutils/include"),
}

var py3CythonOutputIncludes = []VFS{
	Source("contrib/tools/cython/generated_c_headers.h"),
	Source("contrib/tools/cython/generated_cpp_headers.h"),
	Source("contrib/libs/python/Include/compile.h"),
	Source("contrib/libs/python/Include/frameobject.h"),
	Source("contrib/libs/python/Include/longintrepr.h"),
	Source("contrib/libs/python/Include/pyconfig.h"),
	Source("contrib/libs/python/Include/Python.h"),
	Source("contrib/libs/python/Include/pythread.h"),
	Source("contrib/libs/python/Include/structmember.h"),
	Source("contrib/libs/python/Include/traceback.h"),
	Source("contrib/libs/cxxsupp/openmp/omp.h"),
}

func dedupVFS(in []VFS) []VFS {
	seen := make(map[VFS]struct{}, len(in))
	out := make([]VFS, 0, len(in))

	for _, p := range in {
		if _, ok := seen[p]; ok {
			continue
		}

		seen[p] = struct{}{}
		out = append(out, p)
	}

	return out
}

var py3CythonEmbeddedFiles = []string{
	"contrib/tools/cython/Cython/Utility/arrayarray.h",
	"contrib/tools/cython/Cython/Utility/AsyncGen.c",
	"contrib/tools/cython/Cython/Utility/Buffer.c",
	"contrib/tools/cython/Cython/Utility/Builtins.c",
	"contrib/tools/cython/Cython/Utility/CMath.c",
	"contrib/tools/cython/Cython/Utility/CommonStructures.c",
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
	"contrib/tools/cython/Cython/Utility/ModuleSetupCode.c",
	"contrib/tools/cython/Cython/Utility/NumpyImportArray.c",
	"contrib/tools/cython/Cython/Utility/ObjectHandling.c",
	"contrib/tools/cython/Cython/Utility/Optimize.c",
	"contrib/tools/cython/Cython/Utility/Overflow.c",
	"contrib/tools/cython/Cython/Utility/Printing.c",
	"contrib/tools/cython/Cython/Utility/Profile.c",
	"contrib/tools/cython/Cython/Utility/StringTools.c",
	"contrib/tools/cython/Cython/Utility/Synchronization.c",
	"contrib/tools/cython/Cython/Utility/TestUtilityLoader.c",
	"contrib/tools/cython/Cython/Utility/TypeConversion.c",
	"contrib/tools/cython/Cython/Utility/UFuncs_C.c",
	"contrib/tools/cython/Cython/Utility/arrayarray.h",
}
