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
		// Track CF-source extensions: when an INFile is a CF (CONFIGURE_FILE)
		// output, upstream's JV node lists both the CF source (e.g. Cpp.stg.in)
		// and the configure_file.py script as inputs alongside the dst. The
		// CF entry's SourcePath was set at registration time (emit_cf.go).
		var cfExtraInputs []VFS
		deduper.reset()
		appendCFExtra := func(v VFS) {
			if !deduper.add(v) {
				return
			}

			cfExtraInputs = append(cfExtraInputs, v)
		}

		for _, inTok := range run.INFiles {
			vfs := copyFileInputVFS(ctx.fs, instance.Path.rel(), inTok)
			inVFSByToken[inTok] = vfs
			inputs = append(inputs, vfs)

			if reg != nil {
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
			vfs := copyFileOutputVFS(instance.Path.rel(), outTok)
			outVFSByToken[outTok] = vfs
			outputs = append(outputs, vfs)
		}

		for _, outTok := range run.OUTNoAutoFiles {
			vfs := copyFileOutputVFS(instance.Path.rel(), outTok)
			outVFSByToken[outTok] = vfs
			outputs = append(outputs, vfs)
		}

		depRefs := resolveCodegenDepRefsExt(ctx, instance, nil, inputs)
		args := antlrRunCmdArgs(instance, run, inVFSByToken, outVFSByToken)
		cwd := ""

		if run.CWD != nil {
			cwd = expandRunProgramCWD(instance, *run.CWD)
		}

		jvRef := EmitJVGeneral(instance, jarVFS, args, inputs, outputs, cwd, depRefs, cfModuleTag(d, instance), d.tc, ctx.emit)

		if reg != nil {
			// The JV node's full $(S) input set = source-rooted IN/CF inputs plus
			// the two implicit sources EmitJVGeneral appends (stdout2stderr.py and
			// the antlr jar). Consumers compiling a JV output (e.g. a PB protoc
			// node fed JsonPathParser.proto) inherit these as transitive sources.
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
			if !isCCSourceExt(outTok) {
				continue
			}

			outVFS := outVFSByToken[outTok]
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

	for _, a := range run.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${ARCADIA_BUILD_ROOT}", "$(B)")
		a = strings.ReplaceAll(a, "${CURDIR}", instance.Path.String())
		a = strings.ReplaceAll(a, "${BINDIR}", Build(instance.Path.rel()).String())
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path.rel())
		a = strings.ReplaceAll(a, "$CURDIR", instance.Path.String())
		a = strings.ReplaceAll(a, "$BINDIR", Build(instance.Path.rel()).String())

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
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
		// ANTLR combined-grammar convention (matches the reference graph): the
		// generated *Lexer.cpp's compile reaches the paired *Parser.cpp — the
		// parser TU is what carries the protobuf AST header, and the lexer
		// delegates to it. The parser .cpp pulls the proto header directly via
		// OUTPUT_INCLUDES. Neither generated .cpp lists the sibling generated .h
		// as an input (the lexer→parser edge is one-directional).
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
		// For the generated *Lexer.h header: register the paired *Parser.cpp
		// directly (not the *Lexer.cpp sibling) so that cross-module CC nodes
		// that include the header reach *Parser.cpp without listing *Lexer.cpp
		// as an input. The *Lexer.cpp sibling is compiled separately by the ANTLR
		// module and must not appear in other modules' CC inputs.
		base := strings.TrimSuffix(outTok, filepath.Ext(outTok))

		if parserBase, isLexerH := strings.CutSuffix(base, "Lexer"); isLexerH {
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if parserVFS, ok := outVFSByToken[parserBase+"Parser"+ext]; ok {
					appendUnique(parserVFS.rel())
					break
				}
			}
		} else {
			// Non-Lexer headers: register the sibling .cpp (general case)
			for _, ext := range []string{".cpp", ".cc", ".cxx", ".c"} {
				if cppVFS, ok := outVFSByToken[base+ext]; ok {
					appendUnique(cppVFS.rel())
					break
				}
			}
		}
	}

	for _, input := range inputs {
		// $(B)-rooted inputs are generator intermediates the RUN_ANTLR step
		// itself consumed (e.g. the CONFIGURE_FILE'd protobuf.stg). Consumers
		// that walk this output's closure (the proto-split RUN_PROGRAM protoc
		// node, downstream CC) reach those intermediates through the producer
		// dep edge, not as transitive source inputs — upstream lists only the
		// $(S) leaf (the .stg's source .stg.in, also in `inputs`). Emitting the
		// $(B) intermediate over-includes it and diverges the consumer self_uid.
		if !input.isSource() {
			continue
		}

		appendUnique(input.rel())
	}

	appendUnique(stdout2stderrVFS.rel())
	appendUnique(jarVFS.rel())

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

	if strings.HasPrefix(outVFS.rel(), prefix) {
		return strings.TrimPrefix(outVFS.rel(), prefix)
	}

	ThrowFmt("gen: antlr output %q is outside module %q", outVFS.rel(), modulePath)
	return ""
}
