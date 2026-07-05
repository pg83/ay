package main

import (
	"path/filepath"
	"strings"
)

func (e *EmitContext) emitAntlr4GrammarStmt(g Antlr4GrammarInfo) {
	ctx, instance, d := e.ctx, e.instance, e.d
	outPrefix := instance.Path.rel() + "/"

	if g.IsSplit {
		jvRef := emitJVSplit(instance, g.Lexer, g.Parser, g.Visitor, g.Listener, d.unit.CCTag, d.tc, ctx.emit)
		lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
		parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
		lexerG4 := source(instance.Path.rel(), "/", g.Lexer)
		parserG4 := source(instance.Path.rel(), "/", g.Parser)
		lexerCpp := build(outPrefix, lexerBase, ".cpp")
		parserCpp := build(outPrefix, parserBase, ".cpp")

		e.codegen.register(&GeneratedFileInfo{
			OutputPath:     lexerCpp,
			ProducerRef:    jvRef,
			GeneratorRefs:  nil,
		})

		e.codegen.register(&GeneratedFileInfo{
			OutputPath:     parserCpp,
			ProducerRef:    jvRef,
			GeneratorRefs:  nil,
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

			e.codegen.register(&GeneratedFileInfo{
				OutputPath:     build(outPrefix, suffix),
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
			})
		}

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

		e.emitJVDownstreamCPCC(jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes)
	} else {
		jvRef := emitJV(instance, g.Grammar, g.Options, g.Visitor, g.Listener, d.unit.CCTag, d.tc, ctx.emit)
		base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
		grammarG4 := source(instance.Path.rel(), "/", g.Grammar)
		lexerCpp := build(outPrefix, base, "Lexer.cpp")
		parserCpp := build(outPrefix, base, "Parser.cpp")

		e.codegen.register(&GeneratedFileInfo{
			OutputPath:     lexerCpp,
			ProducerRef:    jvRef,
			GeneratorRefs:  nil,
		})

		e.codegen.register(&GeneratedFileInfo{
			OutputPath:     parserCpp,
			ProducerRef:    jvRef,
			GeneratorRefs:  nil,
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

			e.codegen.register(&GeneratedFileInfo{
				OutputPath:     build(outPrefix, suffix),
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
			})
		}

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

		e.emitJVDownstreamCPCC(jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes)
	}
}
