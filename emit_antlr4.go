package main

import (
	"path/filepath"
	"strings"
)

func (e *EmitContext) emitAntlr4GrammarStmt(g Antlr4GrammarInfo) {
	ctx, instance, d := e.ctx, e.instance, e.d
	outPrefix := instance.Path.relString() + "/"
	includes := []VFS{antlr4RuntimeHeaderVFS}

	for _, inc := range g.OutputIncludes {
		includes = append(includes, source(inc))
	}

	parsed := antlrWitnessParsed(ctx.na, includes)

	if g.IsSplit {
		jvRef := ctx.emit.reserve()
		tc := d.tc

		jvPE := func() {
			e.emitJVSplitReserved(g.Lexer, g.Parser, g.Visitor, g.Listener, tc, jvRef)
		}

		pending := e.pendingEmit(jvPE)
		lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
		parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
		lexerCpp := build(outPrefix, lexerBase, ".cpp")
		parserCpp := build(outPrefix, parserBase, ".cpp")

		e.register(GeneratedFileInfo{
			OutputPath:      lexerCpp,
			ProducerRef:     jvRef,
			ProducerMainOut: lexerCpp,
			GeneratorRefs:   nil,
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
			OnUse:           pending,
		})

		e.register(GeneratedFileInfo{
			OutputPath:      parserCpp,
			ProducerRef:     jvRef,
			ProducerMainOut: lexerCpp,
			GeneratorRefs:   nil,
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
			OnUse:           pending,
		})

		for _, suffix := range []string{
			lexerBase + ".h",
			parserBase + ".h",
			parserBase + "Visitor.h",
			parserBase + "BaseVisitor.h",
		} {
			e.register(GeneratedFileInfo{
				OutputPath:      build(outPrefix, suffix),
				ProducerRef:     jvRef,
				ProducerMainOut: lexerCpp,
				GeneratorRefs:   nil,
				ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
				OnUse:           pending,
			})
		}

		cpccPairs := []struct{ cpp, h VFS }{
			{build(outPrefix, lexerBase, ".cpp"), build(outPrefix, lexerBase, ".h")},
			{build(outPrefix, parserBase, ".cpp"), build(outPrefix, parserBase, ".h")},
		}

		e.emitJVDownstreamCPCC(cpccPairs, g.OutputIncludes)
	} else {
		jvRef := ctx.emit.reserve()
		tc := d.tc

		jvPE := func() {
			e.emitJVReserved(g.Grammar, g.Options, g.Visitor, g.Listener, tc, jvRef)
		}

		pending := e.pendingEmit(jvPE)
		base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
		lexerCpp := build(outPrefix, base, "Lexer.cpp")
		parserCpp := build(outPrefix, base, "Parser.cpp")

		e.register(GeneratedFileInfo{
			OutputPath:      lexerCpp,
			ProducerRef:     jvRef,
			ProducerMainOut: lexerCpp,
			GeneratorRefs:   nil,
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
			OnUse:           pending,
		})

		e.register(GeneratedFileInfo{
			OutputPath:      parserCpp,
			ProducerRef:     jvRef,
			ProducerMainOut: lexerCpp,
			GeneratorRefs:   nil,
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
			OnUse:           pending,
		})

		for _, suffix := range []string{
			base + "Lexer.h",
			base + "Parser.h",
			base + "Visitor.h",
			base + "BaseVisitor.h",
		} {
			e.register(GeneratedFileInfo{
				OutputPath:      build(outPrefix, suffix),
				ProducerRef:     jvRef,
				ProducerMainOut: lexerCpp,
				GeneratorRefs:   nil,
				ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: parsed},
				OnUse:           pending,
			})
		}

		cpccPairs := []struct{ cpp, h VFS }{
			{build(outPrefix, base, "Lexer.cpp"), build(outPrefix, base, "Lexer.h")},
			{build(outPrefix, base, "Parser.cpp"), build(outPrefix, base, "Parser.h")},
		}

		e.emitJVDownstreamCPCC(cpccPairs, g.OutputIncludes)
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
