package main

import (
	"strings"
)

func (e *EmitContext) emitAntlrRunStmt(run AntlrRunInfo) {
	ctx, instance, d := e.ctx, e.instance, e.d
	jarVFS := antlr4JarVFS

	if run.Macro == "RUN_ANTLR" {
		jarVFS = antlr3JarVFS
	}

	if run.Jar != 0 {
		jarVFS = e.requireProducedInput("ANTLR jar", run.Jar.string(), copyFileInputVFS(ctx.fs, instance.Path, run.Jar.string()))
	}

	inVFSByToken := make(map[string]VFS, len(run.INFiles))
	inputs := make([]VFS, 0, len(run.INFiles))
	inputSources := make([]VFS, 0, len(run.INFiles))
	inputBuilds := make([]VFS, 0, len(run.INFiles))
	hiddenInputs := make([]VFS, 0, len(run.INFiles))

	for _, inTok := range run.INFiles {
		vfs := e.requireProducedInput("IN", inTok.string(), copyFileInputVFS(ctx.fs, instance.Path, inTok.string()))

		if run.DeclSeq != 0 {
			vfs = e.runProgramInputVFS(inTok.string())
		}

		inVFSByToken[inTok.string()] = vfs
		inputs = append(inputs, vfs)

		if run.DeclSeq != 0 && inTok != run.Args[0] {
			hiddenInputs = append(hiddenInputs, vfs)
		} else if vfs.isBuild() {
			inputBuilds = append(inputBuilds, vfs)
		} else {
			inputSources = append(inputSources, vfs)
		}
	}

	outVFSByToken := make(map[string]VFS, len(run.OUTFiles)+len(run.OUTNoAutoFiles))
	outputs := make([]VFS, 0, len(run.OUTFiles)+len(run.OUTNoAutoFiles))

	for _, outTok := range run.OUTFiles {
		vfs := copyFileOutputVFS(instance.Path.relString(), outTok.string())

		outVFSByToken[outTok.string()] = vfs
		outputs = append(outputs, vfs)
	}

	for _, outTok := range run.OUTNoAutoFiles {
		vfs := copyFileOutputVFS(instance.Path.relString(), outTok.string())

		outVFSByToken[outTok.string()] = vfs
		outputs = append(outputs, vfs)
	}

	args := antlrRunCmdArgs(instance, run, inVFSByToken, outVFSByToken)
	cwd := ""

	if run.CWD != nil {
		cwd = run.CWD.string()
	}

	jvRef := ctx.emit.reserve()
	tc := d.tc
	inputSourcesSnap := ctx.na.vfsList(inputSources...)
	inputBuildsSnap := ctx.na.vfsList(inputBuilds...)
	outputsSnap := ctx.na.vfsList(outputs...)
	hiddenSnap := ctx.na.vfsList(hiddenInputs...)

	pe := func() {
		e.emitJVGeneralReserved(jarVFS, args, inputSourcesSnap, inputBuildsSnap, hiddenSnap, outputsSnap, cwd, tc, jvRef)
	}

	pending := e.pendingEmit(pe)

	if run.DeclSeq != 0 {
		e.collectCPPStyleInputs(inputs...)
	}

	for _, outVFS := range outVFSByToken {
		e.register(GeneratedFileSpec{
			OutputPath:      outVFS,
			ProducerRef:     jvRef,
			ProducerMainOut: outputs[0],
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: antlrParsedIncludes(instance.Path.relString(), run)},
			OnUse:           pending,
		})
	}

	for _, outTok := range run.OUTFiles {
		if !isCCSourceExt(outTok.string()) {
			continue
		}

		outVFS := outVFSByToken[outTok.string()]
		cppRel := antlrOutputModuleRel(instance.Path.relString(), outVFS)

		e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), cppRel).any(), Prio: stmtPrioDefault})
	}
}

func antlrRunCmdArgs(instance ModuleInstance, run AntlrRunInfo, inVFSByToken, outVFSByToken map[string]VFS) []string {
	args := make([]string, 0, len(run.Args))

	for _, aTok := range run.Args {
		a := aTok.string()

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
		}

		args = append(args, a)
	}

	return args
}

func antlrParsedIncludes(modulePath string, run AntlrRunInfo) []IncludeDirective {
	var parsed []IncludeDirective

	seen := map[string]struct{}{}

	appendUnique := func(target string) {
		if target == "" {
			return
		}

		if _, ok := seen[target]; ok {
			return
		}

		seen[target] = struct{}{}
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(target).any())})
	}

	for _, include := range run.OutputIncludes {
		appendUnique(copyFileIncludeTarget(modulePath, include.string()))
	}

	return parsed
}

func antlrOutputModuleRel(modulePath string, outVFS VFS) string {
	prefix := modulePath + "/"

	if strings.HasPrefix(outVFS.relString(), prefix) {
		return strings.TrimPrefix(outVFS.relString(), prefix)
	}

	panic(exceptionf("gen: antlr output %q is outside module %q", outVFS.relString(), modulePath))
}
