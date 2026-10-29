package main

import (
	"sort"
	"strings"
)

var cfKV = KV{P: pkCF, PC: pcYellow}

func (e *EmitContext) emitExplicitCF(cf *ConfigureFileStmt) {
	ctx, instance := e.ctx, e.instance
	srcVFS := e.requireProducedInput("CONFIGURE_FILE src", cf.Src, copyFileInputVFS(ctx.fs, instance.Path, cf.Src))
	outVFS := copyFileOutputVFS(instance.Path.relString(), cf.Dst)

	e.emitConfigureFile(srcVFS, outVFS)
}

func (e *EmitContext) emitLibraryHInSource(meta SrcMeta) {
	src := meta.Source
	instance := e.instance
	srcRel := e.moduleSourceRel(src)
	srcVFS := e.sourceForEmission(meta)
	outVFS := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, ".in"))

	e.emitConfigureFile(srcVFS, outVFS)
}

func (e *EmitContext) emitLibraryCInSource(meta SrcMeta) {
	instance := e.instance
	srcRel := e.moduleSourceRel(meta.Source)
	srcVFS := e.sourceForEmission(meta)
	outVFS := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, ".in"))

	e.emitConfigureFile(srcVFS, outVFS)

	meta.Source = outVFS.any()
	e.enqueueSrc(meta)
}

func (e *EmitContext) emitConfigureFile(srcVFS, outVFS VFS) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.emit.nodeArenas()
	env := envVarsVCS

	var cfgVars []string

	if srcVFS.isSource() {
		cfgVars = buildCFGVars(ctx.fs, srcVFS.relString(), d.cc.SetVars, d.cc.DefaultVars, instance.Platform.BuildTypeUpperSTR.string())
	}

	python3 := d.cc.TC.Python3
	cfRef := ctx.emit.reserve()
	parsed := e.scanner.parsedBucketForInput(srcVFS, parsedIncludesLocal, nil)

	pe := func() {
		cmdArgs := na.anys.alloc(4 + len(cfgVars))[:0]

		cmdArgs = append(cmdArgs, python3.any(), configureFilePyVFS.any(), srcVFS.any(), outVFS.any())
		cmdArgs = appendInternAnys(cmdArgs, cfgVars)
		na.anys.commit(len(cmdArgs))

		cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

		inputs := na.inputList(na.vfsList(configureFilePyVFS), na.vfsList(srcVFS))

		e.emitReservedNode(NodeSpec{Node: Node{Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:  env, Inputs: inputs, KV: &cfKV,
			Outputs:   na.vfsList(outVFS),
			Resources: usesPython3}, input: InputSpec{roots: na.rootInputList(na.vfsList(configureFilePyVFS, srcVFS))},
		}, cfRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:    outVFS,
		SourcePath:    srcVFS,
		ProducerRef:   cfRef,
		GeneratorRefs: nil,

		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},

		OnUse: pending,
	})

	return cfRef
}

func buildCFGVars(fs FS, rel string, setVars, defaultVars map[STR]STR, buildTypeUpper string) []string {
	referenced := map[string]bool{}
	data := concatChunks(fs.read(rel))

	for _, next := range [...]func([]byte, int) (int, int, int, bool){cfgVarRefNext, cfgCmakeDefineNext} {
		for pos := 0; ; {
			ns, ne, end, ok := next(data, pos)

			if !ok {
				break
			}

			referenced[string(data[ns:ne])] = true
			pos = end
		}
	}

	var vars []string

	for name := range referenced {
		k := internStr(name)

		switch {
		case mapHas(setVars, k):
			vars = append(vars, name+"="+trimSurroundingQuotes(setVars[k].string()))
		case mapHas(defaultVars, k):
			vars = append(vars, name+"="+trimSurroundingQuotes(defaultVars[k].string()))
		case name == "BUILD_TYPE":
			vars = append(vars, "BUILD_TYPE="+buildTypeUpper)
		}
	}

	sort.Strings(vars)

	return vars
}

func mapHas(m map[STR]STR, k STR) bool {
	_, ok := m[k]

	return ok
}
