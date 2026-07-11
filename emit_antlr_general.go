package main

import (
	"path/filepath"
	"strings"
)

func (e *EmitContext) emitAntlrRunStmt(run AntlrRunInfo) {
	ctx, instance, d := e.ctx, e.instance, e.d
	reg := e.codegen
	jarVFS := antlr4JarVFS

	if run.Macro == "RUN_ANTLR" {
		jarVFS = antlr3JarVFS
	}

	inVFSByToken := make(map[string]VFS, len(run.INFiles))
	inputs := make([]VFS, 0, len(run.INFiles))

	var cfExtraInputs []VFS

	for _, inTok := range run.INFiles {
		vfs := e.requireProducedInput("IN", inTok.string(), copyFileInputVFS(ctx.fs, instance.Path, inTok.string()))

		inVFSByToken[inTok.string()] = vfs
		inputs = append(inputs, vfs)

		if info := reg.use(vfs); info != nil {
			for _, v := range info.SourceInputs {
				cfExtraInputs = append(cfExtraInputs, v)
			}
		}
	}

	cfExtraInputs = dedupInPlace(cfExtraInputs)
	inputs = append(inputs, cfExtraInputs...)

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
	ccTag := d.unit.CCTag
	tc := d.tc
	inputsSnap := ctx.na.vfsList(inputs...)
	outputsSnap := ctx.na.vfsList(outputs...)

	pe := func() {
		e.emitJVGeneralReserved(jarVFS, args, inputsSnap, outputsSnap, cwd, ccTag, tc, jvRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	jvSourceInputs := ctx.na.vfs.alloc(len(inputs) + 2)[:0]

	for _, v := range inputs {
		if v.isSource() {
			jvSourceInputs = append(jvSourceInputs, v)
		}
	}

	jvSourceInputs = append(jvSourceInputs, stdout2stderrVFS, jarVFS)
	ctx.na.vfs.commit(len(jvSourceInputs))

	jvSourceInputs = jvSourceInputs[:len(jvSourceInputs):len(jvSourceInputs)]

	for outTok, outVFS := range outVFSByToken {
		e.register(GeneratedFileInfo{
			OutputPath:     outVFS,
			ProducerRef:    jvRef,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: antlrParsedIncludes(instance.Path.relString(), run, outTok, outVFSByToken, inputs, jarVFS)},
			SourceInputs:   jvSourceInputs,
			OnUse:          pending,
		})
	}

	for _, outTok := range run.OUTFiles {
		if !isCCSourceExt(outTok.string()) {
			continue
		}

		outVFS := outVFSByToken[outTok.string()]
		cppRel := antlrOutputModuleRel(instance.Path.relString(), outVFS)

		e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), cppRel).any(), Prio: stmtPrioDefault, Bucket: bkAntlr})
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

func antlrParsedIncludes(modulePath string, run AntlrRunInfo, outTok string, outVFSByToken map[string]VFS, inputs []VFS, jarVFS VFS) []IncludeDirective {
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

	if isCCSourceExt(outTok) {
		base := strings.TrimSuffix(outTok, filepath.Ext(outTok))

		if parserBase, isLexer := strings.CutSuffix(base, "Lexer"); isLexer {
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if parserVFS, ok := outVFSByToken[parserBase+"Parser"+ext]; ok {
					appendUnique(parserVFS.relString())

					break
				}
			}
		}
	} else if isHeaderSource(outTok) {
		base := strings.TrimSuffix(outTok, filepath.Ext(outTok))

		if parserBase, isLexerH := strings.CutSuffix(base, "Lexer"); isLexerH {
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if parserVFS, ok := outVFSByToken[parserBase+"Parser"+ext]; ok {
					appendUnique(parserVFS.relString())

					break
				}
			}
		} else {
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if cppVFS, ok := outVFSByToken[base+ext]; ok {
					appendUnique(cppVFS.relString())

					break
				}
			}
		}
	}

	for _, input := range inputs {
		if !input.isSource() {
			continue
		}

		appendUnique(input.relString())
	}

	appendUnique(stdout2stderrVFS.relString())
	appendUnique(jarVFS.relString())

	for _, include := range run.OutputIncludes {
		appendUnique(copyFileIncludeTarget(modulePath, include.string()))
	}

	if len(parsed) == 0 {
		return nil
	}

	return parsed
}

func antlrOutputModuleRel(modulePath string, outVFS VFS) string {
	prefix := modulePath + "/"

	if strings.HasPrefix(outVFS.relString(), prefix) {
		return strings.TrimPrefix(outVFS.relString(), prefix)
	}

	throwFmt("gen: antlr output %q is outside module %q", outVFS.relString(), modulePath)

	return ""
}
