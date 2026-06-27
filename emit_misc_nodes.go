package main

import (
	"path/filepath"
	"strings"
)

func emitMiscNodes(ctx *GenCtx, instance ModuleInstance, d *ModuleData, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS) {
	outPrefix := instance.Path.rel() + "/"

	for _, cf := range d.configureFiles {
		emitExplicitCF(ctx, instance, cf, d)
	}

	antlrCCRefs, antlrCCOutputs := emitAntlrRuns(ctx, instance, d, consumerInputs)

	ccRefs = append(ccRefs, antlrCCRefs...)

	ccOutputs = append(ccOutputs, antlrCCOutputs...)

	for _, g := range d.antlr4Grammars {
		if g.IsSplit {
			jvRef := emitJVSplit(instance, g.Lexer, g.Parser, g.Visitor, g.Listener, cfModuleTag(d, instance), d.tc, ctx.emit)
			lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
			parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
			lexerG4 := source(instance.Path.rel(), "/", g.Lexer)
			parserG4 := source(instance.Path.rel(), "/", g.Parser)
			lexerCpp := build(outPrefix, lexerBase, ".cpp")
			parserCpp := build(outPrefix, parserBase, ".cpp")

			ctx.codegenFor(instance).register(&GeneratedFileInfo{
				ProducerKvP:    pkJV,
				OutputPath:     lexerCpp,
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: nil,
			})

			ctx.codegenFor(instance).register(&GeneratedFileInfo{
				ProducerKvP:    pkJV,
				OutputPath:     parserCpp,
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: nil,
			})

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
				parsed := make([]IncludeDirective, 0, len(witnessIncludes))

				for _, include := range witnessIncludes {
					parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: internStr(include.rel())})
				}

				ctx.codegenFor(instance).register(&GeneratedFileInfo{
					ProducerKvP:    pkJV,
					OutputPath:     build(outPrefix, suffix),
					ProducerRef:    jvRef,
					GeneratorRefs:  nil,
					ParsedIncludes: parsed,
				})
			}

			if consumerInputs != nil {
				jvInputs := []VFS{
					source(instance.Path.rel(), "/", g.Lexer),
					source(instance.Path.rel(), "/", g.Parser),
					stdout2stderrVFS,
					antlr4JarVFS,
				}

				jvPrimary := build(outPrefix, lexerBase, ".cpp")

				cpccPairs := []struct{ cpp, h VFS }{
					{build(outPrefix, lexerBase, ".cpp"), build(outPrefix, lexerBase, ".h")},
					{build(outPrefix, parserBase, ".cpp"), build(outPrefix, parserBase, ".h")},
				}

				refs, outs := emitJVDownstreamCPCC(ctx, instance, jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes, *consumerInputs)

				ccRefs = append(ccRefs, refs...)

				ccOutputs = append(ccOutputs, outs...)
			}
		} else {
			jvRef := emitJV(instance, g.Grammar, g.Options, g.Visitor, g.Listener, cfModuleTag(d, instance), d.tc, ctx.emit)
			base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
			grammarG4 := source(instance.Path.rel(), "/", g.Grammar)
			lexerCpp := build(outPrefix, base, "Lexer.cpp")
			parserCpp := build(outPrefix, base, "Parser.cpp")

			ctx.codegenFor(instance).register(&GeneratedFileInfo{
				ProducerKvP:    pkJV,
				OutputPath:     lexerCpp,
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: nil,
			})

			ctx.codegenFor(instance).register(&GeneratedFileInfo{
				ProducerKvP:    pkJV,
				OutputPath:     parserCpp,
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: nil,
			})

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
				parsed := make([]IncludeDirective, 0, len(witnessIncludes))

				for _, include := range witnessIncludes {
					parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: internStr(include.rel())})
				}

				ctx.codegenFor(instance).register(&GeneratedFileInfo{
					ProducerKvP:    pkJV,
					OutputPath:     build(outPrefix, suffix),
					ProducerRef:    jvRef,
					GeneratorRefs:  nil,
					ParsedIncludes: parsed,
				})
			}

			if consumerInputs != nil {
				jvInputs := []VFS{
					source(instance.Path.rel(), "/", g.Grammar),
					stdout2stderrVFS,
					antlr4JarVFS,
				}

				jvPrimary := build(outPrefix, base, "Lexer.cpp")

				cpccPairs := []struct{ cpp, h VFS }{
					{build(outPrefix, base, "Lexer.cpp"), build(outPrefix, base, "Lexer.h")},
					{build(outPrefix, base, "Parser.cpp"), build(outPrefix, base, "Parser.h")},
				}

				refs, outs := emitJVDownstreamCPCC(ctx, instance, jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes, *consumerInputs)

				ccRefs = append(ccRefs, refs...)

				ccOutputs = append(ccOutputs, outs...)
			}
		}
	}

	if d.createBuildInfoFor != nil {
		biRef := emitBI(instance, d.createBuildInfoFor.string(), biFlagsForInstance(instance.Platform), d.tc, ctx.emit)

		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			ProducerKvP:   pkBI,
			OutputPath:    build(outPrefix, d.createBuildInfoFor.string()),
			ProducerRef:   biRef,
			GeneratorRefs: nil,
			ParsedIncludes: []IncludeDirective{
				{kind: includeQuoted, target: internStr(buildInfoGenPyVFS.rel())},
				{kind: includeQuoted, target: internStr(xargsPyVFS.rel())},
				{kind: includeQuoted, target: internStr(yieldLinePyVFS.rel())},
			},
		})
	}

	return
}
