package main

import (
	"strings"
)

var (
	genPy3RegScriptPath = genPy3RegScriptVFS.String()
)

func emitPySrcs(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.pySrcs) == 0 {
		return
	}

	if d.pyBuildNoPYC {
		return
	}

	py3ccLDRef, py3ccRaw := ctx.tool(argToolsPy3ccBin)
	py3ccBinary := canonicalizePy3ccBinary(py3ccRaw)

	py3ccSlowLDRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)

	ctx.tool(argToolsRescompilerBin)
	ctx.tool(argToolsRescompressorBin)
	ctx.tool(argToolsArchiver)

	for _, srcRel := range d.pySrcs {
		if strings.HasSuffix(srcRel, ".pyi") {
			continue
		}

		generatedInputs := d.pyGeneratedSrcs[srcRel]
		srcAbs := resolveSourceVFS(ctx, instance, srcRel, d.srcDir)

		if generatedInputs != nil {
			srcAbs = Build(instance.Path + "/" + srcRel)
		}

		moduleName := srcAbs.Rel() + "-"

		var outputPath VFS

		if strings.Contains(srcRel, "/") {
			outputPath = Build(instance.Path + "/" + srcRel + "." + pySrcYapycSuffix(instance.Path) + ".yapyc3")
		} else {
			outputPath = Build(instance.Path + "/" + srcRel + ".yapyc3")
		}

		cmdArgs := []STR{
			(py3ccBinary).str(),
			argSlowPy3cc.str(),
			(py3ccSlowBin).str(),
			internStr(moduleName),
			(srcAbs).str(),
			(outputPath).str(),
		}

		env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}, {Name: "PYTHONHASHSEED", Value: "0"}}
		inputs := []VFS{py3ccBinary, py3ccSlowBin, srcAbs}

		if generatedInputs != nil {
			inputs = []VFS{srcAbs}
			inputs = append(inputs, generatedInputs...)
			inputs = append(inputs, py3ccBinary, py3ccSlowBin)

			if len(inputs) > 4 {
				toolA := inputs[len(inputs)-2]
				toolB := inputs[len(inputs)-1]
				copy(inputs[4:], inputs[2:len(inputs)-2])
				inputs[2] = toolA
				inputs[3] = toolB
			}

			inputs = dedupVFS(inputs)
		}

		node := &Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  inputs,
			Outputs: []VFS{outputPath},
			KV:      KV{P: pkPY, PC: pcYellow},
			TargetProperties: func() TargetProperties {
				tp := TargetProperties{ModuleDir: instance.Path}

				if d.moduleStmt.Name == tokPy23Library {
					tp.ModuleTag = "py3"
				}

				// PY3_BIN_LIB submodule of PY3_PROGRAM bundles pysrc bytecode
				// under its lowercased MODULE_TAG, matching the surrounding
				// objcopy/global.a target_properties.
				if d.programPairedLib {
					tp.ModuleTag = "py3_bin_lib"
				}

				return tp
			}(),
			Requirements: Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		}

		var toolRefs []NodeRef

		if py3ccLDRef != (NodeRef(0)) {
			node.DepRefs = append(node.DepRefs, py3ccLDRef)
			toolRefs = append(toolRefs, py3ccLDRef)
		}

		if py3ccSlowLDRef != (NodeRef(0)) {
			node.DepRefs = append(node.DepRefs, py3ccSlowLDRef)
			toolRefs = append(toolRefs, py3ccSlowLDRef)
		}

		if generatedInputs != nil {
			if extras := resolveCodegenDepRefsExt(ctx, instance, nil, inputs, node.DepRefs...); len(extras) > 0 {
				node.DepRefs = append(node.DepRefs, extras...)
			}
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = toolRefs
		}

		pyRef := ctx.emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform))

		registerBoundGeneratedParsedOutput(ctx, instance, "PY", outputPath, nil, pyRef, toolRefs)
	}
}

type pyRegisterResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitPyRegister(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs, py3Suffix bool) *pyRegisterResult {
	if len(d.pyRegister) == 0 {
		return nil
	}

	res := &pyRegisterResult{}

	for i, arg := range d.pyRegister {
		priorShort := make(map[string]struct{}, i)

		for j := 0; j < i; j++ {
			if j < len(d.pyRegisterExplicit) && !d.pyRegisterExplicit[j] {
				continue
			}

			prior := d.pyRegister[j]
			priorShort[prior[strings.LastIndexByte(prior, '.')+1:]] = struct{}{}
		}

		regCpp := arg + ".reg3.cpp"
		regCppVFS := Build(instance.Path + "/" + regCpp)
		regCppAbs := regCppVFS.String()

		env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

		pyCmdArgs := []STR{
			internStr(instance.Platform.Tools.Python3),
			(genPy3RegScriptVFS).str(),
			internStr(arg),
			internStr(regCppAbs),
		}

		pyRef, ok := ctx.pyRegisterOutputs[regCppVFS]

		if !ok {
			pyInstance := instance
			pyInstance.Platform = ctx.target

			pyNode := &Node{
				Cmds: []Cmd{
					{CmdArgs: pyCmdArgs, Env: env},
				},
				Env:              env,
				Inputs:           []VFS{genPy3RegScriptVFS},
				Outputs:          []VFS{regCppVFS},
				KV:               KV{P: pkPY, PC: pcYellow},
				TargetProperties: TargetProperties{ModuleDir: instance.Path},
				Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
				DepRefs:          []NodeRef{},
			}

			if py3Suffix {
				pyNode.TargetProperties.ModuleTag = "py3"
			}

			pyRef = ctx.emit.Emit(bindNodePlatform(withResources(pyNode, resourcePatternYMakePython3), pyInstance.Platform))
			ctx.pyRegisterOutputs[regCppVFS] = pyRef
		}

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{pyRef}
		ccIn.Py3Suffix = py3Suffix

		if len(d.cythonCpp) > 0 {
			ccIn.AddIncl = appendCythonCCAddIncl(ccIn.AddIncl, d.cythonNumpyBeforeInclude)
		}

		ccIn.IncludeInputs = []VFS{genPy3RegScriptVFS}

		if len(in.CFlags) > 0 {
			filtered := make([]ARG, 0, len(in.CFlags))

			for _, f := range in.CFlags {
				if short, ok := pyInitDefineShortname(f.String()); ok {
					if _, keep := priorShort[short]; !keep {
						continue
					}
				}

				filtered = append(filtered, f)
			}

			ccIn.CFlags = filtered
		}

		ccRef, ccOut, _ := EmitCC(instance, regCpp, regCppVFS, ccIn, ctx.host, ctx.emit)

		res.Refs = append(res.Refs, ccRef)
		res.Outputs = append(res.Outputs, ccOut)
	}

	return res
}

func pyInitDefineShortname(flag string) (string, bool) {
	for _, pfx := range []string{"-DPyInit_", "-Dinit_module_"} {
		if strings.HasPrefix(flag, pfx) {
			rest := flag[len(pfx):]

			if eq := strings.IndexByte(rest, '='); eq >= 0 {
				return rest[:eq], true
			}

			return rest, true
		}
	}

	return "", false
}
