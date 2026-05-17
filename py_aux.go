package main

import (
	"strings"
)

type generatedPyAuxChunksResult struct {
	Refs         []NodeRef
	Outputs      []VFS
	MemberInputs []VFS
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

		src := Build(instance.Path + "/" + srcRel)
		entries = append(entries, pyProtoAuxEntry{path: src, key: generatedPyResourceKey(instance.Path, d, srcRel), inputs: genInputs})

		if !d.pyBuildNoPYC {
			suffix := ".yapyc3"
			if strings.Contains(srcRel, "/") {
				suffix = "." + pySrcYapycSuffix(instance.Path) + ".yapyc3"
			}
			yp := Build(instance.Path + "/" + srcRel + suffix)
			entries = append(entries, pyProtoAuxEntry{path: yp, key: generatedPyResourceKey(instance.Path, d, srcRel+".yapyc3"), inputs: genInputs})
		}
	}
	if len(entries) == 0 {
		return nil
	}

	rescompilerRef := walkHostToolForRef(ctx, instance, "tools/rescompiler/bin")
	rawRes := emitRawAuxResourceChunks(ctx, instance, entries, "PY3", nil, rescompilerRef)
	if rawRes == nil {
		return nil
	}

	res := &generatedPyAuxChunksResult{MemberInputs: rawRes.MemberInputs}
	for i, prRef := range rawRes.PRRefs {
		aux := rawRes.PROutputs[i]
		ccIn := in
		ccIn.IsGenerated = true
		ccIn.HasGenerator = true
		ccIn.Generator = prRef
		ccIn.ForceCxx = true
		ccIn.PerSourceCFlags = append(append([]string(nil), in.PerSourceCFlags...), "-x", "c++")
		ccInputs := append([]VFS{aux}, rawRes.MemberInputs...)
		ccInputs = dedupVFS(ccInputs)
		ccIn.IncludeInputs = ccInputs

		ccRef, ccOut := EmitCC(instance, aux.Rel[strings.LastIndex(aux.Rel, "/")+1:], ccIn, ctx.host, ctx.emit)
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
	Refs         []NodeRef
	Outputs      []VFS
	MemberInputs []VFS
	PRRefs       []NodeRef
	PROutputs    []VFS
}

func emitRawAuxResourceChunks(ctx *genCtx, instance ModuleInstance, entries []pyProtoAuxEntry, moduleTag string, deps []NodeRef, rescompilerRef NodeRef) *rawAuxResourceChunksResult {
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
	inputSeen := map[VFS]struct{}{}
	depSeen := map[NodeRef]struct{}{}

	addInput := func(v VFS) {
		if _, ok := inputSeen[v]; ok {
			return
		}
		inputSeen[v] = struct{}{}
		cur.inputs = append(cur.inputs, v)
	}
	addDep := func(ref NodeRef) {
		if ref == (NodeRef{}) {
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
		inputSeen = map[VFS]struct{}{}
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		key := "resfs/file/py/" + e.key
		arcBuildPath := "${ARCADIA_BUILD_ROOT}/" + e.path.Rel
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + arcBuildPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + e.path.Rel

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
	memberSeen := map[VFS]struct{}{}
	for _, ch := range chunks {
		aux := Build(instance.Path + "/" + protoResourceHash(ch.hashInputs, "$S/"+instance.Path, moduleTag) + "_raw.auxcpp")
		cmdArgs := []string{rescompilerBinPath, aux.String()}
		cmdArgs = append(cmdArgs, ch.cmdArgs...)

		chDeps := append([]NodeRef(nil), deps...)
		chDeps = append(chDeps, ch.deps...)
		if rescompilerRef != (NodeRef{}) {
			chDeps = append(chDeps, rescompilerRef)
		}

		inputs := append([]VFS{rescompilerBinVFS}, ch.inputs...)
		if extras := resolveCodegenDepRefsExt(ctx, instance, nil, ch.inputs, chDeps...); len(extras) > 0 {
			chDeps = append(chDeps, extras...)
		}
		ref := ctx.emit.Emit(&Node{
			Cmds:             []Cmd{{CmdArgs: cmdArgs}},
			Inputs:           inputs,
			Outputs:          []VFS{aux},
			KV:               map[string]string{"p": "PR", "pc": "yellow"},
			Tags:             instance.Platform.Tags,
			TargetProperties: map[string]string{"module_dir": instance.Path},
			Platform:         string(instance.Platform.Target),
			HostPlatform:     instance.Platform.IsHost,
			Requirements:     map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
			DepRefs:          chDeps,
		})
		res.Refs = append(res.Refs, ref)
		res.Outputs = append(res.Outputs, aux)
		res.PRRefs = append(res.PRRefs, ref)
		res.PROutputs = append(res.PROutputs, aux)

		for _, v := range inputs {
			if _, ok := memberSeen[v]; ok {
				continue
			}
			memberSeen[v] = struct{}{}
			res.MemberInputs = append(res.MemberInputs, v)
		}
	}

	return res
}
