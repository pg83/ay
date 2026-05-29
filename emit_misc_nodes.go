package main

import (
	"path/filepath"
	"strings"
)

func emitMiscNodes(ctx *genCtx, instance ModuleInstance, d *moduleData, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS) {
	outPrefix := instance.Path + "/"
	reg := codegenRegForInstance(ctx, instance)

	for _, cf := range d.configureFiles {
		emitExplicitCF(ctx, instance, cf, d, reg)
	}

	antlrCCRefs, antlrCCOutputs := emitAntlrRuns(ctx, instance, d, consumerInputs)
	ccRefs = append(ccRefs, antlrCCRefs...)
	ccOutputs = append(ccOutputs, antlrCCOutputs...)

	for _, g := range d.antlr4Grammars {
		if g.IsSplit {
			jvRef := EmitJVSplit(instance, g.Lexer, g.Parser, g.Visitor, g.Listener, cfModuleTag(d, instance), ctx.emit)

			lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
			parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
			if reg != nil {
				lexerG4 := Source(instance.Path + "/" + g.Lexer)
				parserG4 := Source(instance.Path + "/" + g.Parser)
				lexerCpp := Build(outPrefix + lexerBase + ".cpp")
				parserCpp := Build(outPrefix + parserBase + ".cpp")
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", lexerCpp, nil, jvRef)
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", parserCpp, nil, jvRef)
				witnessIncludes := []VFS{
					antlr4RuntimeHeaderVFS,
					lexerCpp,
					stdout2stderrVFS,
					antlr4JarVFS,
					lexerG4,
					parserG4,
				}
				for _, suffix := range []string{
					lexerBase + ".h",
					parserBase + ".h",
					parserBase + "Visitor.h",
					parserBase + "BaseVisitor.h",
				} {
					parsed := make([]includeDirective, 0, len(witnessIncludes))
					for _, include := range witnessIncludes {
						parsed = append(parsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
					}
					registerBoundGeneratedParsedOutput(ctx, instance, "JV", Build(outPrefix+suffix), parsed, jvRef)
				}
			}

			if consumerInputs != nil {
				jvInputs := []VFS{
					Source(instance.Path + "/" + g.Lexer),
					Source(instance.Path + "/" + g.Parser),
					stdout2stderrVFS,
					antlr4JarVFS,
				}
				jvPrimary := Build(outPrefix + lexerBase + ".cpp")
				cpccPairs := []struct{ cpp, h VFS }{
					{Build(outPrefix + lexerBase + ".cpp"), Build(outPrefix + lexerBase + ".h")},
					{Build(outPrefix + parserBase + ".cpp"), Build(outPrefix + parserBase + ".h")},
				}
				refs, outs := emitJVDownstreamCPCC(ctx, instance, jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes, *consumerInputs)
				ccRefs = append(ccRefs, refs...)
				ccOutputs = append(ccOutputs, outs...)
			}
		} else {
			jvRef := EmitJV(instance, g.Grammar, g.Options, g.Visitor, g.Listener, cfModuleTag(d, instance), ctx.emit)

			base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
			if reg != nil {
				grammarG4 := Source(instance.Path + "/" + g.Grammar)
				lexerCpp := Build(outPrefix + base + "Lexer.cpp")
				parserCpp := Build(outPrefix + base + "Parser.cpp")
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", lexerCpp, nil, jvRef)
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", parserCpp, nil, jvRef)
				witnessIncludes := []VFS{
					antlr4RuntimeHeaderVFS,
					lexerCpp,
					stdout2stderrVFS,
					antlr4JarVFS,
					grammarG4,
				}
				for _, suffix := range []string{
					base + "Lexer.h",
					base + "Parser.h",
					base + "Visitor.h",
					base + "BaseVisitor.h",
				} {
					parsed := make([]includeDirective, 0, len(witnessIncludes))
					for _, include := range witnessIncludes {
						parsed = append(parsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
					}
					registerBoundGeneratedParsedOutput(ctx, instance, "JV", Build(outPrefix+suffix), parsed, jvRef)
				}
			}

			if consumerInputs != nil {
				jvInputs := []VFS{
					Source(instance.Path + "/" + g.Grammar),
					stdout2stderrVFS,
					antlr4JarVFS,
				}
				jvPrimary := Build(outPrefix + base + "Lexer.cpp")
				cpccPairs := []struct{ cpp, h VFS }{
					{Build(outPrefix + base + "Lexer.cpp"), Build(outPrefix + base + "Lexer.h")},
					{Build(outPrefix + base + "Parser.cpp"), Build(outPrefix + base + "Parser.h")},
				}
				refs, outs := emitJVDownstreamCPCC(ctx, instance, jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes, *consumerInputs)
				ccRefs = append(ccRefs, refs...)
				ccOutputs = append(ccOutputs, outs...)
			}
		}
	}

	if d.createBuildInfoFor != nil {
		biRef := EmitBI(instance, *d.createBuildInfoFor, biFlagsForInstance(instance.Platform), ctx.emit)

		if reg != nil {
			registerBoundGeneratedParsedOutput(ctx, instance, "BI", Build(outPrefix+*d.createBuildInfoFor), []includeDirective{
				{kind: includeQuoted, target: internString(buildInfoGenPyVFS.Rel())},
				{kind: includeQuoted, target: internString(xargsPyVFS.Rel())},
				{kind: includeQuoted, target: internString(yieldLinePyVFS.Rel())},
			}, biRef)
		}
	}

	_ = d.runPrograms
	return
}
