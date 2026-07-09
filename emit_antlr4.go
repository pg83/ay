package main

import (
	"path/filepath"
	"strings"
)

func (e *EmitContext) emitAntlr4GrammarStmt(g Antlr4GrammarInfo) {
	ctx, instance, d := e.ctx, e.instance, e.d
	outPrefix := instance.Path.relString() + "/"

	if g.IsSplit {
		jvRef := ctx.emit.reserve()
		ccTag := d.unit.CCTag
		tc := d.tc

		jvPE := func() {
			emitJVSplitReserved(instance, g.Lexer, g.Parser, g.Visitor, g.Listener, ccTag, tc, ctx.emit, jvRef)
		}

		lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
		parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
		lexerG4 := source(instance.Path.relString(), "/", g.Lexer)
		parserG4 := source(instance.Path.relString(), "/", g.Parser)
		lexerCpp := build(outPrefix, lexerBase, ".cpp")
		parserCpp := build(outPrefix, parserBase, ".cpp")

		e.codegen.register(GeneratedFileInfo{
			OutputPath:    lexerCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
		}).OnUse = &jvPE

		e.codegen.register(GeneratedFileInfo{
			OutputPath:    parserCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
		}).OnUse = &jvPE

		witnessIncludes := []VFS{
			antlr4RuntimeHeaderVFS,
			lexerCpp,
			stdout2stderrVFS,
			antlr4JarVFS,
			lexerG4,
			parserG4,
		}

		parsed := antlrWitnessParsed(e.ctx.na, witnessIncludes)

		for _, suffix := range []string{
			lexerBase + ".h",
			parserBase + ".h",
			parserBase + "Visitor.h",
			parserBase + "BaseVisitor.h",
		} {
			e.codegen.register(GeneratedFileInfo{
				OutputPath:     build(outPrefix, suffix),
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
			}).OnUse = &jvPE
		}

		jvInputs := []VFS{
			source(instance.Path.relString(), "/", g.Lexer),
			source(instance.Path.relString(), "/", g.Parser),
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
		jvRef := ctx.emit.reserve()
		ccTag := d.unit.CCTag
		tc := d.tc

		jvPE := func() {
			emitJVReserved(instance, g.Grammar, g.Options, g.Visitor, g.Listener, ccTag, tc, ctx.emit, jvRef)
		}

		base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
		grammarG4 := source(instance.Path.relString(), "/", g.Grammar)
		lexerCpp := build(outPrefix, base, "Lexer.cpp")
		parserCpp := build(outPrefix, base, "Parser.cpp")

		e.codegen.register(GeneratedFileInfo{
			OutputPath:    lexerCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
		}).OnUse = &jvPE

		e.codegen.register(GeneratedFileInfo{
			OutputPath:    parserCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
		}).OnUse = &jvPE

		witnessIncludes := []VFS{
			antlr4RuntimeHeaderVFS,
			lexerCpp,
			stdout2stderrVFS,
			antlr4JarVFS,
			grammarG4,
		}

		parsed := antlrWitnessParsed(e.ctx.na, witnessIncludes)

		for _, suffix := range []string{
			base + "Lexer.h",
			base + "Parser.h",
			base + "Visitor.h",
			base + "BaseVisitor.h",
		} {
			e.codegen.register(GeneratedFileInfo{
				OutputPath:     build(outPrefix, suffix),
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
			}).OnUse = &jvPE
		}

		jvInputs := []VFS{
			source(instance.Path.relString(), "/", g.Grammar),
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

func antlrWitnessParsed(na *NodeArenas, witnessIncludes []VFS) []IncludeDirective {
	parsed := na.dirs.alloc(len(witnessIncludes))[:0]

	for _, include := range witnessIncludes {
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(include.rel().any())})
	}

	na.dirs.commit(len(parsed))

	return parsed[:len(parsed):len(parsed)]
}
