package main

type CythonStmt struct {
	Src       string
	Generated string
	Options   []string
}

func emitCythonCpp(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) []*sourceEmit {
	if len(d.cythonCpp) == 0 {
		return nil
	}

	out := make([]*sourceEmit, 0, len(d.cythonCpp))

	for _, stmt := range d.cythonCpp {
		generated := stmt.Generated
		if generated == "" {
			generated = stmt.Src + ".py3.cpp"
		}
		generatedVFS := Build(instance.Path + "/" + generated)
		srcVFS := Source(instance.Path + "/" + stmt.Src)
		inputs := cythonInputs(ctx, instance, srcVFS, in)

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
		cmdArgs = append(cmdArgs,
			"--cplus",
			"-I$(B)",
			"-I$(S)",
			"-I$(S)/contrib/tools/cython/Cython/Includes",
			srcVFS.String(),
			"-o",
			generatedVFS.String(),
		)

		cyRef := ctx.emit.Emit(&Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  inputs,
			Outputs: []VFS{generatedVFS},
			KV: map[string]string{
				"p":  "CY",
				"pc": "yellow",
			},
			Platform:     string(instance.Platform.Target),
			HostPlatform: instance.Platform.IsHost,
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
			Tags: instance.Platform.Tags,
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
		})

		ccIn := in
		ccIn.IsGenerated = true
		ccIn.HasGenerator = true
		ccIn.Generator = cyRef
		ccIn.Py3Suffix = stmt.Generated == "" && d.moduleStmt != nil && resourceModuleTag(d.moduleStmt.Name) != ""
		ccIn.IncludeInputs = inputs

		ccRef, ccOut := EmitCC(instance, generated, ccIn, ctx.host, ctx.emit)
		ccInputs := append([]VFS{generatedVFS}, inputs...)

		out = append(out, &sourceEmit{
			Ref:          ccRef,
			OutPath:      ccOut,
			CcIns:        ccInputs,
			PrimaryCount: 1,
		})
	}

	return out
}

func cythonInputs(ctx *genCtx, instance ModuleInstance, src VFS, in ModuleCCInputs) []VFS {
	inputs := make([]VFS, 0, 2+len(py3CythonEmbeddedFiles))
	inputs = append(inputs, Source("contrib/tools/cython/cython.py"))

	for _, rel := range py3CythonEmbeddedFiles {
		inputs = append(inputs, Source(rel))
	}

	inputs = append(inputs, src)
	inputs = append(inputs, walkClosure(ctx, instance, src, in)...)

	return dedupVFS(inputs)
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
	"contrib/tools/cython/Cython/Utility/TestCythonScope.pyx",
	"contrib/tools/cython/Cython/Utility/TestCyUtilityLoader.pyx",
	"contrib/tools/cython/Cython/Utility/TestUtilityLoader.c",
	"contrib/tools/cython/Cython/Utility/UFuncs_C.c",
}
