package main

import (
	"path/filepath"
	"strings"
)

func emitAntlrRuns(ctx *genCtx, instance ModuleInstance, d *moduleData, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS) {
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
		for _, inTok := range run.INFiles {
			vfs := copyFileInputVFS(ctx.fs, instance.Path, inTok)
			inVFSByToken[inTok] = vfs
			inputs = append(inputs, vfs)
		}

		outVFSByToken := make(map[string]VFS, len(run.OUTFiles)+len(run.OUTNoAutoFiles))
		outputs := make([]VFS, 0, len(run.OUTFiles)+len(run.OUTNoAutoFiles))
		for _, outTok := range run.OUTFiles {
			vfs := copyFileOutputVFS(instance.Path, outTok)
			outVFSByToken[outTok] = vfs
			outputs = append(outputs, vfs)
		}
		for _, outTok := range run.OUTNoAutoFiles {
			vfs := copyFileOutputVFS(instance.Path, outTok)
			outVFSByToken[outTok] = vfs
			outputs = append(outputs, vfs)
		}

		depRefs := resolveCodegenDepRefsExt(ctx, instance, nil, inputs)
		args := antlrRunCmdArgs(instance, run, inVFSByToken, outVFSByToken)
		cwd := ""
		if run.CWD != nil {
			cwd = expandRunProgramCWD(instance, *run.CWD)
		}

		jvRef := EmitJVGeneral(instance, jarVFS, args, inputs, outputs, cwd, depRefs, ctx.emit)

		if reg != nil {
			for outTok, outVFS := range outVFSByToken {
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", outVFS, antlrParsedIncludes(instance.Path, run, outTok, outVFSByToken, inputs, jarVFS), jvRef)
			}
		}

		if consumerInputs == nil {
			continue
		}

		for _, outTok := range run.OUTFiles {
			if !isCCSourceExt(outTok) {
				continue
			}
			outVFS := outVFSByToken[outTok]
			cppRel := antlrOutputModuleRel(instance.Path, outVFS)
			ccRef, ccOut := emitCodegenDownstreamCC(ctx, instance, cppRel, nil, []NodeRef{jvRef}, *consumerInputs)
			ccRefs = append(ccRefs, ccRef)
			ccOutputs = append(ccOutputs, ccOut)
		}
	}

	return ccRefs, ccOutputs
}

func antlrRunCmdArgs(instance ModuleInstance, run antlrRunInfo, inVFSByToken, outVFSByToken map[string]VFS) []string {
	args := make([]string, 0, len(run.Args))
	for _, a := range run.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${ARCADIA_BUILD_ROOT}", "$(B)")
		a = strings.ReplaceAll(a, "${CURDIR}", Source(instance.Path).String())
		a = strings.ReplaceAll(a, "${BINDIR}", Build(instance.Path).String())
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path)
		a = strings.ReplaceAll(a, "$CURDIR", Source(instance.Path).String())
		a = strings.ReplaceAll(a, "$BINDIR", Build(instance.Path).String())
		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		}
		args = append(args, a)
	}
	return args
}

func antlrParsedIncludes(modulePath string, run antlrRunInfo, outTok string, outVFSByToken map[string]VFS, inputs []VFS, jarVFS VFS) []includeDirective {
	var parsed []includeDirective
	seen := map[string]struct{}{}
	appendUnique := func(target string) {
		if target == "" {
			return
		}
		if _, ok := seen[target]; ok {
			return
		}
		seen[target] = struct{}{}
		parsed = append(parsed, includeDirective{kind: includeQuoted, target: target})
	}

	if isCCSourceExt(outTok) {
		if headerTok := strings.TrimSuffix(outTok, filepath.Ext(outTok)) + ".h"; headerTok != outTok {
			if headerVFS, ok := outVFSByToken[headerTok]; ok {
				appendUnique(headerVFS.Rel)
			}
		}
	} else if isHeaderSource(outTok) {
		base := strings.TrimSuffix(outTok, filepath.Ext(outTok))
		for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
			if cppVFS, ok := outVFSByToken[base+ext]; ok {
				appendUnique(cppVFS.Rel)
				break
			}
		}
	}

	for _, input := range inputs {
		appendUnique(input.Rel)
	}
	appendUnique(stdout2stderrVFS.Rel)
	appendUnique(jarVFS.Rel)
	for _, include := range run.OutputIncludes {
		appendUnique(copyFileIncludeTarget(modulePath, include))
	}

	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

func antlrOutputModuleRel(modulePath string, outVFS VFS) string {
	prefix := modulePath + "/"
	if strings.HasPrefix(outVFS.Rel, prefix) {
		return strings.TrimPrefix(outVFS.Rel, prefix)
	}
	ThrowFmt("gen: antlr output %q is outside module %q", outVFS.Rel, modulePath)
	return ""
}
