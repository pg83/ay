package main

import (
	"strings"
)

var (
	genPy3RegScriptPath = genPy3RegScriptVFS.string()
	// genPy3RegScriptChunk is the reg-script input chunk shared by every
	// py-register node, referenced instead of allocated per node.
	genPy3RegScriptChunk = []VFS{genPy3RegScriptVFS}
)

func emitPySrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	na := ctx.na

	if len(d.pySrcs) == 0 {
		return
	}

	if d.pyBuildNoPYC {
		return
	}

	py3ccLDRef, py3ccBinary := ctx.tool(argToolsPy3cc)

	py3ccSlowLDRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)

	ctx.tool(argToolsRescompiler)
	ctx.tool(argToolsRescompressor)
	ctx.tool(argToolsArchiver)

	// The py3cc tool pair is loop-invariant — one chunk shared by every pysrc node.
	py3ccToolsChunk := []VFS{py3ccBinary, py3ccSlowBin}
	// So is the command head.
	py3ccArgHead := []STR{(py3ccBinary).str(), argSlowPy3cc.str(), (py3ccSlowBin).str()}

	reg := codegenRegForInstance(ctx, instance)

	for _, srcRel := range d.pySrcs {
		if strings.HasSuffix(srcRel.string(), ".pyi") {
			continue
		}

		genInfo := reg.lookupSplit(dirKey(instance.Path.rel()), srcRel)

		var generatedInputs []VFS

		if genInfo != nil {
			generatedInputs = genInfo.SourceInputs
		}

		srcAbs := resolveSourceVFS(ctx, instance, srcRel.string(), d.srcDirs)

		if genInfo != nil {
			srcAbs = build(instance.Path.rel() + "/" + srcRel.string())
		}

		moduleName := srcAbs.rel() + "-"

		var outputPath VFS

		if strings.Contains(srcRel.string(), "/") {
			outputPath = build(instance.Path.rel() + "/" + srcRel.string() + "." + pySrcYapycSuffix(instance.Path.rel()) + ".yapyc3")
		} else {
			outputPath = build(instance.Path.rel() + "/" + srcRel.string() + ".yapyc3")
		}

		cmdArgs := na.chunkList(py3ccArgHead, na.strList(internStr(moduleName),
			(srcAbs).str(),
			(outputPath).str()))

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}
		nodeInputs := na.inputList(py3ccToolsChunk, na.srcChunk(srcAbs))

		var inputs []VFS

		if genInfo != nil {
			// The generated-src input list interleaves the shared generator
			// inputs around the tool pair and dedups across the whole — stays
			// flat (resolveCodegenDepRefs below consumes it flat too).
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
			nodeInputs = na.inputList(inputs)
		}

		node := &Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
				Env: env}),
			Env:     env,
			Inputs:  nodeInputs,
			Outputs: na.vfsList(outputPath),
			KV:      KV{P: pkPY, PC: pcYellow},
			TargetProperties: func() TargetProperties {
				tp := TargetProperties{ModuleDir: instance.Path.rel()}

				if d.moduleStmt.Name == tokPy23Library {
					tp.ModuleTag = tagPy3
				}

				// PY3_BIN_LIB submodule of PY3_PROGRAM bundles pysrc bytecode
				// under its lowercased MODULE_TAG, matching the surrounding
				// objcopy/global.a target_properties.
				if d.programPairedLib {
					tp.ModuleTag = tagPy3BinLib
				}

				return tp
			}(),
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}

		toolRefs := depRefs(py3ccLDRef, py3ccSlowLDRef)

		if genInfo != nil {
			if extras := resolveCodegenDepRefs(ctx, instance, inputs, toolRefs...); len(extras) > 0 {
				node.DepRefs = append(node.DepRefs, extras...)
			}
		}

		node.ForeignDepRefs = toolRefs

		pyRef := ctx.emit.emit(node)

		registerBoundGeneratedParsedOutput(ctx, instance, pkPY, outputPath, nil, pyRef, toolRefs)
	}
}

type PyRegisterResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitPyRegister(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs, py3Suffix bool) *PyRegisterResult {
	na := ctx.na

	if len(d.pyRegister) == 0 {
		return nil
	}

	res := &PyRegisterResult{}

	for i, arg := range d.pyRegister {
		priorShort := make(map[string]struct{}, i)

		for j := 0; j < i; j++ {
			if j < len(d.pyRegisterExplicit) && !d.pyRegisterExplicit[j] {
				continue
			}

			prior := d.pyRegister[j]
			priorStr := prior.string()
			priorShort[priorStr[strings.LastIndexByte(priorStr, '.')+1:]] = struct{}{}
		}

		regCpp := arg.string() + ".reg3.cpp"
		regCppVFS := build(instance.Path.rel() + "/" + regCpp)
		regCppAbs := regCppVFS.string()

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		pyCmdArgs := []STR{
			d.tc.Python3,
			(genPy3RegScriptVFS).str(),
			internStr(arg.string()),
			internStr(regCppAbs),
		}

		// gen_py3_reg.py is platform-independent codegen; upstream attributes such a
		// command to the TARGET platform (a cross build shows every .reg3.cpp as the
		// target ISA, never the host/tool ISA, even for a python module also built
		// for the host). Emit under ctx.target so the target and host instances that
		// reach this output produce byte-identical nodes that collapse by uid — no
		// cross-platform dedup map needed. The per-instance compile of the generated
		// source is emitted below with its own platform.
		pyNode := &Node{
			Platform:         ctx.target,
			Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(pyCmdArgs), Env: env}),
			Env:              env,
			Inputs:           na.inputList(genPy3RegScriptChunk),
			Outputs:          na.vfsList(regCppVFS),
			KV:               KV{P: pkPY, PC: pcYellow},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:        usesPython3,
		}

		if py3Suffix {
			// A PY23_NATIVE_LIBRARY's reg node carries the py3_native tag, like its
			// other objects; plain py3 libraries use py3.
			if d.moduleStmt.Name == tokPy23NativeLibrary {
				pyNode.TargetProperties.ModuleTag = tagPy3Native
			} else {
				pyNode.TargetProperties.ModuleTag = tagPy3
			}
		}

		pyRef := ctx.emit.emit(pyNode)

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{pyRef}
		ccIn.Py3Suffix = py3Suffix

		if len(d.cythonCpp) > 0 {
			ccIn.AddIncl = appendCythonCCAddIncl(ccIn.AddIncl, d.cythonNumpyBeforeInclude)
		}

		// IncludeInputs is the full input window: the compiled register cpp
		// itself plus the generator script.
		ccIn.IncludeInputs = []VFS{regCppVFS, genPy3RegScriptVFS}

		if len(in.CFlags) > 0 {
			filtered := make([]ARG, 0, len(in.CFlags))

			for _, f := range in.CFlags {
				if short, ok := pyInitDefineShortname(f.string()); ok {
					if _, keep := priorShort[short]; !keep {
						continue
					}
				}

				filtered = append(filtered, f)
			}

			ccIn.CFlags = filtered
		}

		// AddIncl/CFlags feed the module-stable arg blocks — rebuild for this copy.
		ccIn.CCBlocks = composeCCModuleArgBlocks(na, instance.Platform, &ccIn)
		ccRef, ccOut, _ := emitCC(instance, regCpp, regCppVFS, ccIn, ctx.host, ctx.emit)

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
