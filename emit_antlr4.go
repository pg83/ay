package main

import (
	"path/filepath"
	"strings"
)

func (e *EmitContext) emitAntlr4GrammarStmt(g Antlr4GrammarInfo) {
	ctx, instance, d := e.ctx, e.instance, e.d
	outPrefix := instance.Path.relString() + "/"

	// IN files (e.g. the customized ANTLR .stg templates copied next to the
	// grammar) must be materialized in the JV node's sandbox: ANTLR loads
	// template overrides from its working directory.
	inVFSs := make([]VFS, 0, len(g.INFiles))

	for _, in := range g.INFiles {
		inVFSs = append(inVFSs, e.requireProducedInput("IN", in, copyFileInputVFS(ctx.fs, instance.Path, in)))
	}

	if g.IsSplit {
		jvRef := ctx.emit.reserve()
		ccTag := d.unit.CCTag
		tc := d.tc

		jvPE := func() {
			e.emitJVSplitReserved(g.Lexer, g.Parser, g.Visitor, g.Listener, ccTag, tc, jvRef, inVFSs)
		}
		pending := e.ctx.na.pendingEmit(jvPE)

		lexerBase := antlrGrammarBase(g.Lexer)
		parserBase := antlrGrammarBase(g.Parser)
		lexerG4 := antlrGrammarVFS(instance, g.Lexer)
		parserG4 := antlrGrammarVFS(instance, g.Parser)
		lexerCpp := build(outPrefix, lexerBase, ".cpp")
		parserCpp := build(outPrefix, parserBase, ".cpp")

		e.register(GeneratedFileInfo{
			OutputPath:    lexerCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
			OnUse:         pending,
		})

		e.register(GeneratedFileInfo{
			OutputPath:    parserCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
			OnUse:         pending,
		})

		witnessIncludes := []VFS{
			antlr4RuntimeHeaderVFS,
			lexerCpp,
			stdout2stderrVFS,
			antlr4JarVFS,
			lexerG4,
			parserG4,
		}

		parsed := antlrWitnessParsed(e.ctx.na, witnessIncludes)

		suffixes := []string{
			lexerBase + ".h",
			parserBase + ".h",
		}

		if g.Visitor {
			suffixes = append(suffixes, parserBase+"Visitor.h", parserBase+"BaseVisitor.h")
		}

		for _, suffix := range suffixes {
			e.register(GeneratedFileInfo{
				OutputPath:     build(outPrefix, suffix),
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
				OnUse:          pending,
			})
		}

		jvInputs := concat([]VFS{
			antlrGrammarVFS(instance, g.Lexer),
			antlrGrammarVFS(instance, g.Parser),
			stdout2stderrVFS,
			antlr4JarVFS,
		}, inVFSs)

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
			e.emitJVReserved(g.Grammar, g.Options, g.Visitor, g.Listener, ccTag, tc, jvRef, inVFSs)
		}
		pending := e.ctx.na.pendingEmit(jvPE)

		base := antlrGrammarBase(g.Grammar)
		grammarG4 := antlrGrammarVFS(instance, g.Grammar)
		lexerCpp := build(outPrefix, base, "Lexer.cpp")
		parserCpp := build(outPrefix, base, "Parser.cpp")

		e.register(GeneratedFileInfo{
			OutputPath:    lexerCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
			OnUse:         pending,
		})

		e.register(GeneratedFileInfo{
			OutputPath:    parserCpp,
			ProducerRef:   jvRef,
			GeneratorRefs: nil,
			OnUse:         pending,
		})

		witnessIncludes := []VFS{
			antlr4RuntimeHeaderVFS,
			lexerCpp,
			stdout2stderrVFS,
			antlr4JarVFS,
			grammarG4,
		}

		parsed := antlrWitnessParsed(e.ctx.na, witnessIncludes)

		suffixes := []string{
			base + "Lexer.h",
			base + "Parser.h",
		}

		if g.Visitor {
			suffixes = append(suffixes, base+"Visitor.h", base+"BaseVisitor.h")
		}

		for _, suffix := range suffixes {
			e.register(GeneratedFileInfo{
				OutputPath:     build(outPrefix, suffix),
				ProducerRef:    jvRef,
				GeneratorRefs:  nil,
				ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
				OnUse:          pending,
			})
		}

		jvInputs := concat([]VFS{
			antlrGrammarVFS(instance, g.Grammar),
			stdout2stderrVFS,
			antlr4JarVFS,
		}, inVFSs)

		jvPrimary := build(outPrefix, base, "Lexer.cpp")

		cpccPairs := []struct{ cpp, h VFS }{
			{build(outPrefix, base, "Lexer.cpp"), build(outPrefix, base, "Lexer.h")},
			{build(outPrefix, base, "Parser.cpp"), build(outPrefix, base, "Parser.h")},
		}

		e.emitJVDownstreamCPCC(jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes)
	}
}

// antlrGrammarVFS resolves a grammar reference that may be module-relative or
// an already-rooted VFS path ($(B)/... from ${BINDIR} expansion, $(S)/... etc).
func antlrGrammarVFS(instance ModuleInstance, grammar string) VFS {
	if vfs, ok := moduleRootedVFS(instance.Path.relString(), grammar); ok {
		return vfs
	}

	return source(instance.Path.relString(), "/", grammar)
}

// antlrGrammarBase mirrors ymake's ${noext:GRAMMAR}: strip the last extension
// (.g or .g4) from the grammar file name. ANTLR names its outputs after the
// grammar declaration, so the declared outputs use this base.
func antlrGrammarBase(grammar string) string {
	b := filepath.Base(grammar)

	return strings.TrimSuffix(b, filepath.Ext(b))
}

func antlrWitnessParsed(na *NodeArenas, witnessIncludes []VFS) []IncludeDirective {
	parsed := na.dirs.alloc(len(witnessIncludes))[:0]

	for _, include := range witnessIncludes {
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(include.rel().any())})
	}

	na.dirs.commit(len(parsed))

	return parsed[:len(parsed):len(parsed)]
}
