package main

import "strings"

type generatedPyAuxChunksResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitGeneratedPyAuxChunks(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) *generatedPyAuxChunksResult {
	if len(d.pyGeneratedSrcs) == 0 {
		return nil
	}

	var entries []pyProtoAuxEntry

	for _, srcRel := range d.pySrcs {
		genInputs := d.pyGeneratedSrcs[srcRel]

		if genInputs == nil {
			continue
		}

		src := Build(instance.Path.Rel() + "/" + srcRel)
		entries = append(entries, pyProtoAuxEntry{path: src, key: generatedPyResourceKey(instance.Path.Rel(), d, srcRel), inputs: genInputs})

		if !d.pyBuildNoPYC {
			suffix := ".yapyc3"

			if strings.Contains(srcRel, "/") {
				suffix = "." + pySrcYapycSuffix(instance.Path.Rel()) + ".yapyc3"
			}

			yp := Build(instance.Path.Rel() + "/" + srcRel + suffix)
			entries = append(entries, pyProtoAuxEntry{path: yp, key: generatedPyResourceKey(instance.Path.Rel(), d, srcRel+".yapyc3"), inputs: genInputs})
		}
	}

	if len(entries) == 0 {
		return nil
	}

	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	rawRes := emitRawAuxResourceChunks(ctx, instance, entries, "PY3", nil, in, rescompilerRef)

	if rawRes == nil {
		return nil
	}

	res := &generatedPyAuxChunksResult{}

	for i, prRef := range rawRes.PRRefs {
		aux := rawRes.PROutputs[i]
		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{prRef}
		ccIn.ForceCxx = true
		ccIn.PerSourceCFlags = append(append([]ARG(nil), in.PerSourceCFlags...), argX, argC)
		ccIn.IncludeInputs = rawRes.AuxClosures[i]

		ccRef, ccOut, _ := EmitCC(instance, aux.Rel()[strings.LastIndex(aux.Rel(), "/")+1:], aux, ccIn, ctx.host, ctx.emit)
		res.Refs = append(res.Refs, ccRef)
		res.Outputs = append(res.Outputs, ccOut)
	}

	return res
}

func generatedPyResourceKey(modulePath string, d *moduleData, srcRel string) string {
	keyPrefix := ""

	if !d.pyTopLevel {
		keyPrefix = modulePath + "/"
	}

	return keyPrefix + srcRel
}

type rawAuxResourceChunksResult struct {
	Refs        []NodeRef
	Outputs     []VFS
	PRRefs      []NodeRef
	PROutputs   []VFS
	AuxClosures [][]VFS
}

func emitRawAuxResourceChunks(ctx *genCtx, instance ModuleInstance, entries []pyProtoAuxEntry, moduleTag string, deps []NodeRef, in ModuleCCInputs, rescompilerRef NodeRef) *rawAuxResourceChunksResult {
	if len(entries) == 0 {
		return nil
	}

	type chunk struct {
		hashInputs []string
		cmdArgs    []string
		inputs     []VFS
		deps       []NodeRef
	}

	var chunks []chunk
	cur := chunk{}
	cmdLen := 0
	// Chunk accumulation runs no deduper user (pyProtoSourceInputs / the input
	// tail filter below follow the final flush), so the input set lives on the
	// deduper, reset per flush. depSeen stays a local map: it is live
	// simultaneously with the input set.
	deduper.reset()
	depSeen := map[NodeRef]struct{}{}
	addInput := func(v VFS) {
		if !deduper.add(v) {
			return
		}

		cur.inputs = append(cur.inputs, v)
	}
	addDep := func(ref NodeRef) {
		if ref == (NodeRef(0)) {
			return
		}

		if _, ok := depSeen[ref]; ok {
			return
		}

		depSeen[ref] = struct{}{}
		cur.deps = append(cur.deps, ref)
	}
	flush := func() {
		if cmdLen == 0 {
			return
		}

		chunks = append(chunks, cur)
		cur = chunk{}
		cmdLen = 0
		deduper.reset()
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		key := "resfs/file/py/" + e.key
		arcBuildPath := "${ARCADIA_BUILD_ROOT}/" + e.path.Rel()
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + arcBuildPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + e.path.Rel()

		cur.hashInputs = append(cur.hashInputs, "-", kvHash)
		cur.cmdArgs = append(cur.cmdArgs, "-", kvCmd)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len("-") + len(kvHash)

		if cmdLen >= maxCmdLen {
			flush()
		}

		cur.hashInputs = append(cur.hashInputs, arcBuildPath, "-"+key)
		cur.cmdArgs = append(cur.cmdArgs, e.path.String(), "-"+key)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len(arcBuildPath) + len(key)

		if cmdLen >= maxCmdLen {
			flush()
		}
	}

	flush()
	res := &rawAuxResourceChunksResult{}

	for _, ch := range chunks {
		aux := Build(instance.Path.Rel() + "/" + protoResourceHash(ch.hashInputs, "$S/"+instance.Path.Rel(), moduleTag) + "_raw.auxcpp")
		sourceInputs := pyProtoSourceInputs(ch.inputs)
		auxClosure := rawAuxInputClosure(ctx, instance, aux, sourceInputs, in)
		cmdArgs := []STR{internStr(rescompilerBinPath), (aux).str()}
		cmdArgs = appendInternStrs(cmdArgs, ch.cmdArgs)

		chDeps := append([]NodeRef(nil), deps...)
		chDeps = append(chDeps, ch.deps...)

		if rescompilerRef != (NodeRef(0)) {
			chDeps = append(chDeps, rescompilerRef)
		}

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		// ch.inputs is internally deduped already (deduper-gated accumulation),
		// so it survives a whole-list dedup intact — reference it as a chunk and
		// filter only the rescompiler + closure tail against it.
		deduper.reset()

		for _, p := range ch.inputs {
			deduper.add(p)
		}

		tail := make([]VFS, 0, 1+len(auxClosure))

		if deduper.add(rescompilerBinVFS) {
			tail = append(tail, rescompilerBinVFS)
		}

		// auxClosure is the aux window (root-led: aux is a build output); the
		// PR node's own output never joins its inputs, so skip the root.
		for _, p := range auxClosure {
			if p == aux {
				continue
			}

			if deduper.add(p) {
				tail = append(tail, p)
			}
		}

		inputs := inputChunks{ch.inputs, tail}

		if extras := resolveCodegenDepRefsExt(ctx, instance, nil, ch.inputs, chDeps...); len(extras) > 0 {
			chDeps = append(chDeps, extras...)
		}

		ref := ctx.emit.Emit(&Node{
			Platform:         instance.Platform,
			Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
			Env:              env,
			Inputs:           inputs,
			Outputs:          []VFS{aux},
			KV:               KV{P: pkPR, PC: pcYellow, ShowOut: true},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:          chDeps,
		})
		bindGeneratedOutput(ctx, instance, aux, ref)
		res.Refs = append(res.Refs, ref)
		res.Outputs = append(res.Outputs, aux)
		res.PRRefs = append(res.PRRefs, ref)
		res.PROutputs = append(res.PROutputs, aux)
		res.AuxClosures = append(res.AuxClosures, auxClosure)
	}

	return res
}

func rawAuxInputClosure(ctx *genCtx, instance ModuleInstance, aux VFS, seed []VFS, in ModuleCCInputs) []VFS {
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)

	emits := make([]includeDirective, 0, len(seed))

	for _, v := range seed {
		emits = append(emits, includeDirective{kind: includeQuoted, target: internStr(v.Rel())})
	}

	registerGeneratedParsedOutput(ctx, instance, pkPR, aux, emits, []NodeRef{rescompilerRef})

	closure := walkClosure(ctx, instance, aux, in)

	if len(closure) == 0 {
		return nil
	}

	// walkClosure already returns a deduplicated window — no further dedup needed.
	return closure
}
