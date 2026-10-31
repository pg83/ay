package main

import (
	"bytes"
	"slices"
	"strings"
)

var cfKV = KV{P: pkCF, PC: pcYellow}

func (e *EmitContext) emitExplicitCF(cf *ConfigureFileStmt) {
	ctx, instance := e.ctx, e.instance
	srcVFS := copyFileInputVFS(ctx.fs, instance.Path, cf.Src)

	if candidate := moduleInputCandidateVFS(instance.Path.relString(), cf.Src); srcVFS.isSource() && !ctx.fs.isFileVFS(srcVFS) && e.codegen.lookup(candidate) != nil {
		srcVFS = candidate
	}

	srcVFS = e.requireProducedInput("CONFIGURE_FILE src", cf.Src, srcVFS)

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
		cfgVars = buildCFGVars(ctx.fs, srcVFS.relString(), Environment{s: &e.frame.ifEnv})
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

func buildCFGVars(fs FS, rel string, env Environment) []string {
	names := cfgVarNames(concatChunks(fs.read(rel)))
	vars := make([]string, len(names))

	for i, name := range names {
		vars[i] = name + "=" + cfgVarValue(env, name)
	}

	return vars
}

func cfgVarNames(data []byte) []string {
	var names []string

	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))

		if words := bytes.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' }); len(words) >= 2 && (string(words[0]) == "#cmakedefine01" || string(words[0]) == "#cmakedefine") {
			names = append(names, string(words[1]))
		}

		for p2 := -1; ; {
			p1 := bytes.IndexByte(line[p2+1:], '@')

			if p1 < 0 {
				break
			}

			p1 += p2 + 1

			q := bytes.IndexByte(line[p1+1:], '@')

			if q < 0 {
				break
			}

			p2 = p1 + 1 + q

			if found := line[p1+1 : p2]; len(found) > 0 && !bytes.ContainsFunc(found, func(r rune) bool { return !(r == '-' || r < 128 && isIdentByte(byte(r))) }) {
				names = append(names, string(found))
			}
		}
	}

	slices.Sort(names)

	return slices.Compact(names)
}

func cfgVarValue(env Environment, name string) string {
	value := env.expand("$" + name)

	if strings.ContainsAny(value, " \t") {
		return ""
	}

	out := make([]byte, 0, len(value))

	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c == '\\' && i+1 < len(value):
			i++
			out = append(out, value[i])
		case c == '"' || c == '\'':
		default:
			out = append(out, c)
		}
	}

	return string(out)
}
