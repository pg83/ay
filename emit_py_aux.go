package main

import "strings"

var pyAuxKV = KV{P: pkPR, PC: pcYellow, ShowOut: true}

type GeneratedPyAuxChunksResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitGeneratedPyAuxChunks() *GeneratedPyAuxChunksResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	if len(d.pySrcs) == 0 {
		return nil
	}

	reg := ctx.codegenFor(instance)

	var entries []PyProtoAuxEntry

	for i, srcRel := range d.pySrcs {
		info := reg.lookupSplit(dirKey(instance.Path.rel()), srcRel)

		if info == nil {
			continue
		}

		if i >= len(d.pySrcsFullName) || !d.pySrcsFullName[i] {
			continue
		}

		genInputs := info.SourceInputs
		src := build(instance.Path.rel(), "/", srcRel.string())

		entries = append(entries, PyProtoAuxEntry{path: src, key: generatedPyResourceKey(instance.Path.rel(), d, srcRel.string()), inputs: genInputs})

		if !d.pyBuildNoPYC {
			suffix := ".yapyc3"

			if strings.Contains(srcRel.string(), "/") {
				suffix = "." + d.pyYapycSuffix + ".yapyc3"
			}

			yp := build(instance.Path.rel(), "/", srcRel.string(), suffix)

			entries = append(entries, PyProtoAuxEntry{path: yp, key: generatedPyResourceKey(instance.Path.rel(), d, srcRel.string()+".yapyc3"), inputs: genInputs})
		}
	}

	if len(entries) == 0 {
		return nil
	}

	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	rawRes := e.emitRawAuxResourceChunks(entries, "PY3", nil, rescompilerRef)

	if rawRes == nil {
		return nil
	}

	res := &GeneratedPyAuxChunksResult{}

	for _, aux := range rawRes.PROutputs {
		if se := e.emitOneSource(aux.str()); se != nil {
			res.Refs = append(res.Refs, se.Ref)
			res.Outputs = append(res.Outputs, se.OutPath)
		}
	}

	return res
}

func generatedPyResourceKey(modulePath string, d *ModuleData, srcRel string) string {
	keyPrefix := ""

	if !d.pyTopLevel {
		keyPrefix = modulePath + "/"
	}

	return keyPrefix + srcRel
}

type RawAuxResourceChunksResult struct {
	Refs        []NodeRef
	Outputs     []VFS
	PRRefs      []NodeRef
	PROutputs   []VFS
	AuxClosures [][]VFS
}

func (e *EmitContext) emitRawAuxResourceChunks(entries []PyProtoAuxEntry, moduleTag string, deps []NodeRef, rescompilerRef NodeRef) *RawAuxResourceChunksResult {
	ctx, instance, _ := e.ctx, e.instance, e.d
	na := ctx.na

	if len(entries) == 0 {
		return nil
	}

	res := &RawAuxResourceChunksResult{}

	for _, ch := range chunkAuxEntries(entries) {
		aux := build(instance.Path.rel(), "/", protoResourceHash(ch.hashInputs, "$S/"+instance.Path.rel(), moduleTag), "_raw.auxcpp")
		auxRef := ctx.emit.reserve()
		sourceInputs := pyProtoSourceInputs(ch.inputs)
		auxClosure := e.rawAuxInputClosure(aux, sourceInputs, auxRef)
		cmdArgs := []STR{internStr(rescompilerBinPath), (aux).str()}

		cmdArgs = appendInternStrs(cmdArgs, ch.cmdArgs)

		chDeps := append([]NodeRef(nil), deps...)

		chDeps = append(chDeps, ch.deps...)
		chDeps = append(chDeps, depRefs(rescompilerRef)...)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		deduper.reset()

		for _, p := range ch.inputs {
			deduper.add(p)
		}

		tail := make([]VFS, 0, 1+len(auxClosure))

		if deduper.add(rescompilerBinVFS) {
			tail = append(tail, rescompilerBinVFS)
		}

		for _, p := range auxClosure {
			if p == aux {
				continue
			}

			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		inputs := na.inputList(ch.inputs, tail)

		chDeps = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, ch.inputs, chDeps...)

		ctx.emit.emitReserved(&Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:          env,
			Inputs:       inputs,
			Outputs:      na.vfsList(aux),
			KV:           &pyAuxKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:      chDeps,
		}, auxRef)

		res.Refs = append(res.Refs, auxRef)
		res.Outputs = append(res.Outputs, aux)
		res.PRRefs = append(res.PRRefs, auxRef)
		res.PROutputs = append(res.PROutputs, aux)
		res.AuxClosures = append(res.AuxClosures, auxClosure)
	}

	return res
}

func (e *EmitContext) rawAuxInputClosure(aux VFS, seed []VFS, ref NodeRef) []VFS {
	ctx, instance, d := e.ctx, e.instance, e.d
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	emits := make([]IncludeDirective, 0, len(seed))

	for _, v := range seed {
		emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
	}

	var psc []ARG

	if p := d.perSrcCFlagsFor(aux.str()); p != nil {
		psc = *p
	}

	ctx.codegenFor(instance).register(&GeneratedFileInfo{
		OutputPath:     aux,
		ProducerRef:    ref,
		GeneratorRefs:  []NodeRef{rescompilerRef},
		ParsedIncludes: emits,
		Compile:        &CompileSpec{FlatOutput: d.flatSrc(aux.str()), ForceCxx: true, CFlags: concat(psc, []ARG{argX, argC})},
	})

	closure := walkClosure(ctx.scannerFor(instance), aux, d.cc.ScanCfg)

	if len(closure) == 0 {
		return nil
	}

	return closure
}
