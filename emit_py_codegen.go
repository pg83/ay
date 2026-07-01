package main

import (
	"strings"
)

var (
	genPy3RegScriptPath  = genPy3RegScriptVFS.string()
	genPy3RegScriptChunk = []VFS{genPy3RegScriptVFS}
	pyCodegenKV          = KV{P: pkPY, PC: pcYellow}
)

func (e *EmitContext) emitPySrcs() {
	ctx, instance, d := e.ctx, e.instance, e.d
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

	py3ccToolsChunk := []VFS{py3ccBinary, py3ccSlowBin}
	py3ccArgHead := []STR{(py3ccBinary).str(), argSlowPy3cc.str(), (py3ccSlowBin).str()}
	reg := ctx.codegenFor(instance)

	for i, srcRel := range d.pySrcs {
		if extIsPyi(srcRel.string()) {
			continue
		}

		genInfo := reg.lookupSplit(dirKey(instance.Path.rel()), srcRel)

		var generatedInputs []VFS

		if genInfo != nil {
			generatedInputs = genInfo.SourceInputs
		}

		srcAbs := resolveSourceVFS(ctx, instance, srcRel.string(), d.srcDirs)
		moduleName := srcAbs.rel() + "-"

		if genInfo != nil {
			srcAbs = build(instance.Path.rel(), "/", srcRel.string())

			if i < len(d.pySrcsFullName) && d.pySrcsFullName[i] {
				moduleName = srcAbs.rel() + "-"
			} else {
				moduleName = srcRel.string() + "-"
			}
		}

		var outputPath VFS

		if strings.Contains(srcRel.string(), "/") {
			outputPath = build(instance.Path.rel(), "/", srcRel.string(), ".", d.pyYapycSuffix, ".yapyc3")
		} else {
			outputPath = build(instance.Path.rel(), "/", srcRel.string(), ".yapyc3")
		}

		cmdArgs := na.chunkList(py3ccArgHead, na.strList(internStr(moduleName),
			(srcAbs).str(),
			(outputPath).str()))

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}
		nodeInputs := na.inputList(py3ccToolsChunk, na.srcChunk(srcAbs))

		var inputs []VFS

		if genInfo != nil {
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

			nodeInputs = na.inputList(inputs)
		}

		node := &Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
				Env: env}),
			Env:          env,
			Inputs:       nodeInputs,
			Outputs:      na.vfsList(outputPath),
			KV:           &pyCodegenKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}

		toolRefs := depRefs(py3ccLDRef, py3ccSlowLDRef)

		if genInfo != nil {
			if extras := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, inputs); len(extras) > 0 {
				node.DepRefs = append(node.DepRefs, extras...)
			}
		}

		node.ForeignDepRefs = toolRefs

		pyRef := ctx.emit.emit(node)

		reg.register(&GeneratedFileInfo{
			OutputPath:     outputPath,
			ProducerRef:    pyRef,
			GeneratorRefs:  toolRefs,
			ParsedIncludes: nil,
		})
	}
}

type PyRegisterResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitPyRegister(py3Suffix bool) *PyRegisterResult {
	ctx, instance, d := e.ctx, e.instance, e.d
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
		regCppVFS := build(instance.Path.rel(), "/", regCpp)
		regCppAbs := regCppVFS.string()
		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		pyCmdArgs := []STR{
			d.tc.Python3,
			(genPy3RegScriptVFS).str(),
			internStr(arg.string()),
			internStr(regCppAbs),
		}

		pyNode := &Node{
			Platform:     ctx.target,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(pyCmdArgs), Env: env}),
			Env:          env,
			Inputs:       na.inputList(genPy3RegScriptChunk),
			Outputs:      na.vfsList(regCppVFS),
			KV:           &pyCodegenKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}

		pyRef := ctx.emit.emit(pyNode)

		envCFlags := make([]ARG, 0, len(d.cc.CFlags))

		for _, f := range d.cc.CFlags {
			if short, ok := pyInitDefineShortname(f.string()); ok {
				if _, keep := priorShort[short]; !keep {
					continue
				}
			}

			envCFlags = append(envCFlags, f)
		}

		spec := &CompileSpec{Py3Suffix: py3Suffix, EnvCFlags: envCFlags}

		if len(d.cythonCpp) > 0 {
			spec.EnvAddIncl = appendCythonCCAddIncl(d.cc.AddIncl, d.cythonNumpyBeforeInclude)
		}

		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			OutputPath:    regCppVFS,
			ProducerRef:   pyRef,
			ClosureLeaves: []VFS{genPy3RegScriptVFS},
			Compile:       spec,
		})

		se := e.emitOneSource(regCppVFS.str())

		res.Refs = append(res.Refs, se.Ref)
		res.Outputs = append(res.Outputs, se.OutPath)
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
