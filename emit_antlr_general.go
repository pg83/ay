package main

import (
	"path/filepath"
	"strings"
)

func emitAntlrRuns(ctx *GenCtx, instance ModuleInstance, d *ModuleData, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS) {
	if len(d.antlrRuns) == 0 {
		return nil, nil
	}

	reg := codegenRegForInstance(ctx, instance)

	for _, run := range d.antlrRuns {
		jarVFS := antlr4JarVFS

		if run.Macro == "RUN_ANTLR" {
			jarVFS = antlr3JarVFS
		}

		inVFSByToken := make(map[string]VFS, len(run.INFiles))
		inputs := make([]VFS, 0, len(run.INFiles))

		var cfExtraInputs []VFS
		deduper.reset()
		appendCFExtra := func(v VFS) {
			if !deduper.add(v) {
				return
			}

			cfExtraInputs = append(cfExtraInputs, v)
		}

		for _, inTok := range run.INFiles {
			vfs := copyFileInputVFS(ctx.fs, instance.Path.rel(), inTok.string())
			inVFSByToken[inTok.string()] = vfs
			inputs = append(inputs, vfs)

			{
				if info := reg.lookup(vfs); info != nil && info.ProducerKvP == pkCF && info.SourcePath != 0 {
					appendCFExtra(info.SourcePath)
					appendCFExtra(configureFilePyVFS)
				}
			}
		}

		inputs = append(inputs, cfExtraInputs...)

		outVFSByToken := make(map[string]VFS, len(run.OUTFiles)+len(run.OUTNoAutoFiles))
		outputs := make([]VFS, 0, len(run.OUTFiles)+len(run.OUTNoAutoFiles))

		for _, outTok := range run.OUTFiles {
			vfs := copyFileOutputVFS(instance.Path.rel(), outTok.string())
			outVFSByToken[outTok.string()] = vfs
			outputs = append(outputs, vfs)
		}

		for _, outTok := range run.OUTNoAutoFiles {
			vfs := copyFileOutputVFS(instance.Path.rel(), outTok.string())
			outVFSByToken[outTok.string()] = vfs
			outputs = append(outputs, vfs)
		}

		deps := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, inputs)
		args := antlrRunCmdArgs(instance, run, inVFSByToken, outVFSByToken)
		cwd := ""

		if run.CWD != nil {
			cwd = run.CWD.string()
		}

		jvRef := emitJVGeneral(instance, jarVFS, args, inputs, outputs, cwd, deps, cfModuleTag(d, instance), d.tc, ctx.emit)

		{
			jvSourceInputs := make([]VFS, 0, len(inputs)+2)

			for _, v := range inputs {
				if v.isSource() {
					jvSourceInputs = append(jvSourceInputs, v)
				}
			}

			jvSourceInputs = append(jvSourceInputs, stdout2stderrVFS, jarVFS)

			for outTok, outVFS := range outVFSByToken {
				registerBoundGeneratedParsedOutput(ctx, instance, pkJV, outVFS, antlrParsedIncludes(instance.Path.rel(), run, outTok, outVFSByToken, inputs, jarVFS), jvRef, nil)
				reg.setSourceInputs(outVFS, jvSourceInputs)
			}
		}

		if consumerInputs == nil {
			continue
		}

		for _, outTok := range run.OUTFiles {
			if !isCCSourceExt(outTok.string()) {
				continue
			}

			outVFS := outVFSByToken[outTok.string()]
			cppRel := antlrOutputModuleRel(instance.Path.rel(), outVFS)
			ccRef, ccOut := emitCodegenDownstreamCC(ctx, instance, cppRel, []NodeRef{jvRef}, *consumerInputs)
			ccRefs = append(ccRefs, ccRef)
			ccOutputs = append(ccOutputs, ccOut)
		}
	}

	return ccRefs, ccOutputs
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
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: internStr(target)})
	}

	if isCCSourceExt(outTok) {
		base := strings.TrimSuffix(outTok, filepath.Ext(outTok))

		if parserBase, isLexer := strings.CutSuffix(base, "Lexer"); isLexer {
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if parserVFS, ok := outVFSByToken[parserBase+"Parser"+ext]; ok {
					appendUnique(parserVFS.rel())

					break
				}
			}
		}
	} else if isHeaderSource(outTok) {
		base := strings.TrimSuffix(outTok, filepath.Ext(outTok))

		if parserBase, isLexerH := strings.CutSuffix(base, "Lexer"); isLexerH {
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if parserVFS, ok := outVFSByToken[parserBase+"Parser"+ext]; ok {
					appendUnique(parserVFS.rel())

					break
				}
			}
		} else {
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if cppVFS, ok := outVFSByToken[base+ext]; ok {
					appendUnique(cppVFS.rel())

					break
				}
			}
		}
	}

	for _, input := range inputs {
		if !input.isSource() {
			continue
		}

		appendUnique(input.rel())
	}

	appendUnique(stdout2stderrVFS.rel())
	appendUnique(jarVFS.rel())

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

	if strings.HasPrefix(outVFS.rel(), prefix) {
		return strings.TrimPrefix(outVFS.rel(), prefix)
	}

	throwFmt("gen: antlr output %q is outside module %q", outVFS.rel(), modulePath)

	return ""
}
