package main

import "strings"

var (
	flexOutputInclude = IncludeDirective{kind: includeQuoted, target: includeTarget(internStr("util/system/compiler.h").any())}
	flexKV            = KV{P: pkLX, PC: pcYellow}
)

const flexDefaultGenExt = ".cpp"

func flexGeneratedVFS(instance ModuleInstance, srcRel string) VFS {
	if strings.Contains(srcRel, "/") {
		return build(instance.Path.relString(), "/_/", srcRel, flexDefaultGenExt)
	}

	return build(instance.Path.relString(), "/", srcRel, flexDefaultGenExt)
}

func (e *EmitContext) emitLibraryFlexSource(meta SrcMeta) {
	ctx, instance := e.ctx, e.instance
	src := meta.Source
	srcRel := e.moduleSourceRel(src)
	toolArgs, toolBins, toolRefs := e.flexTool()
	srcVFS := e.sourceForEmission(meta)
	outVFS := flexGeneratedVFS(instance, srcRel)
	na := ctx.na
	localBucket := e.scanner.parsedBucketForInput(srcVFS, parsedIncludesCpp, nil)
	parsed := na.dirs.alloc(1 + len(localBucket))[:0]

	parsed = append(parsed, flexOutputInclude)
	parsed = append(parsed, localBucket...)

	na.dirs.commit(len(parsed))
	parsed = parsed[:len(parsed):len(parsed)]

	lxRef := ctx.emit.reserve()

	meta.Source = outVFS.any()
	e.enqueueSrc(meta)

	env := Environment{s: &e.frame.ifEnv}
	lexFlags := na.anyList(configuredCompilerFlags(env.string(envLEX_FLAGS), env)...)

	pe := func() {
		e.emitFlexLX(toolArgs, toolBins, toolRefs, lexFlags, srcVFS, outVFS, lxRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:     outVFS,
		ProducerRef:    lxRef,
		GeneratorRefs:  toolRefs,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		OnUse:          pending,
	})
}

func (e *EmitContext) flexTool() (args []ANY, bins []VFS, refs []NodeRef) {
	for _, word := range strings.Fields(Environment{s: &e.frame.ifEnv}.string(envFlexTool)) {
		prefix, tool, found := strings.Cut(word, `${tool:"`)

		if !found {
			args = append(args, internAny(word))

			continue
		}

		dir, suffix, _ := strings.Cut(tool, `"}`)
		ref, bin := e.tool(internArg(dir))

		if prefix == "" && suffix == "" {
			args = append(args, bin.any())
		} else {
			args = append(args, internV(prefix, bin.prefix(), bin.relString(), suffix).any())
		}

		bins = append(bins, bin)
		refs = append(refs, ref)
	}

	na := e.ctx.na

	return na.anyList(args...), na.vfsList(bins...), na.refList(refs...)
}

func (e *EmitContext) emitFlexLX(toolArgs []ANY, toolBins []VFS, toolRefs []NodeRef, lexFlags []ANY, srcVFS, outVFS VFS, id NodeRef) {
	na := e.ctx.na

	cmdArgs := na.chunkList(toolArgs, lexFlags, na.anyList(
		internV(argDashO.string(), outVFS.prefix(), outVFS.relString()).any(),
		srcVFS.any(),
	))

	env := envVarsVCS

	e.emitReservedNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:  env, Inputs: na.inputList(toolBins, na.vfsList(srcVFS)), Outputs: na.vfsList(outVFS),
		KV:             &flexKV,
		ForeignDepRefs: toolRefs}, input: InputSpec{roots: na.inputList(na.vfsList(srcVFS))},
	}, id)
}
