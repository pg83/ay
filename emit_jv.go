package main

import (
	"path/filepath"
	"strings"
)

// antlr4RuntimeHeaderVFS is the $(S)-rooted antlr4 C++ umbrella
// header included by all ANTLR4-generated .h files; used as static
// EmitsIncludes for JV .h outputs.
var antlr4RuntimeHeaderVFS = Source("contrib/libs/antlr4_cpp_runtime/src/antlr4-runtime.h")
var antlr4RuntimeHeaderPath = antlr4RuntimeHeaderVFS.String()

// antlr4FsToolsVFS / antlr4ProcCmdVFS are build-script helpers
// threaded into JV-derived CP/CC inputs, slotted after the JV primary
// output and before the grammar .g4 files.
var antlr4FsToolsVFS = Source("build/scripts/fs_tools.py")
var antlr4ProcCmdVFS = Source("build/scripts/process_command_files.py")
var antlr4FsToolsPath = antlr4FsToolsVFS.String()
var antlr4ProcCmdFiles = antlr4ProcCmdVFS.String()

// emitJVDownstreamCPCC emits one CP + one CC for each (cpp, h) pair
// from a JV grammar invocation. Pattern: JV outputs CmdLexer.cpp →
// CP renames to CmdLexer.g4.cpp → CC compiles CmdLexer.g4.cpp.o.
//
// outputIncludes (from the macro's OUTPUT_INCLUDES) are attached as
// EmitsIncludes on the CP .g4.cpp so the CC scan walks the transitive
// closure (antlr4-runtime + macro-declared headers).
func emitJVDownstreamCPCC(
	ctx *genCtx,
	instance ModuleInstance,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	cpccPairs []struct{ cpp, h VFS },
	outputIncludes []string,
	in ModuleCCInputs,
) (ccRefs []NodeRef, ccOutputs []VFS) {
	reg := codegenRegForInstance(ctx, instance)

	for _, pair := range cpccPairs {
		srcCpp := pair.cpp
		srcH := pair.h

		// Derive the .g4.cpp name: replace .cpp suffix with .g4.cpp.
		base := strings.TrimSuffix(filepath.Base(srcCpp.Rel()), ".cpp")
		g4CppPath := Build(instance.Path + "/" + base + ".g4.cpp")
		g4CppRel := base + ".g4.cpp"

		// Register .g4.cpp so walkClosure resolves its transitive
		// antlr4-runtime.h chain and the macro's OUTPUT_INCLUDES.
		if reg != nil {
			emits := make([]includeDirective, 0, 1+len(outputIncludes))
			emits = append(emits, includeDirective{kind: includeQuoted, target: antlr4RuntimeHeaderVFS.Rel()})
			for _, h := range outputIncludes {
				emits = append(emits, includeDirective{kind: includeQuoted, target: h})
			}
			registerGeneratedParsedOutput(ctx, instance, "CP", g4CppPath, emits)
		}

		// Compute the include closure from the g4.cpp (through the registry).
		ccIn := in
		ccIn.ExtraDepRefs = nil
		closure := walkClosure(ctx, instance, g4CppPath, ccIn)

		// CP node inputs: [jvPrimary, (srcCpp if != primary), fsTools, procCmd, jvInputs..., closure...]
		cpInputs := make([]VFS, 0, 2+len(jvInputs)+len(closure)+2)
		cpInputs = append(cpInputs, jvPrimary)
		if srcCpp != jvPrimary {
			cpInputs = append(cpInputs, srcCpp)
		}
		cpInputs = append(cpInputs, antlr4FsToolsVFS, antlr4ProcCmdVFS)
		cpInputs = append(cpInputs, jvInputs...)
		cpInputs = append(cpInputs, closure...)

		// The closure minus the cp-specific prefix is the antlr4 content.
		// Pass only the closure part to EmitJVCPG4 (it assembles the prefix itself).
		cpRef := EmitJVCPG4(instance, srcCpp, g4CppPath, jvRef, jvPrimary, jvInputs, closure, ctx.emit)

		// CC node inputs: srcVFS=g4CppPath (Build root) is the input.
		// IncludeInputs = [jvPrimary, srcH, fsTools, procCmd, jvInputs..., closure...]
		ccIncludeInputs := make([]VFS, 0, 3+len(jvInputs)+len(closure)+2)
		ccIncludeInputs = append(ccIncludeInputs, jvPrimary)
		ccIncludeInputs = append(ccIncludeInputs, srcH)
		ccIncludeInputs = append(ccIncludeInputs, antlr4FsToolsVFS, antlr4ProcCmdVFS)
		ccIncludeInputs = append(ccIncludeInputs, jvInputs...)
		ccIncludeInputs = append(ccIncludeInputs, closure...)

		ccIn.IncludeInputs = ccIncludeInputs
		// Deps: [jvRef, cpRef].
		ccIn.ExtraDepRefs = []NodeRef{jvRef, cpRef}
		// ANTLR4-generated .g4.cpp files have per-rule unused locals;
		// `-Wno-unused-variable` silences the `-Werror` diagnostic.
		ccIn.PerSourceCFlags = []string{"-Wno-unused-variable"}

		ccRef, ccOut, _ := EmitCC(instance, g4CppRel, g4CppPath, ccIn, ctx.host, ctx.emit)

		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)
		_ = cpInputs // assembled inside EmitJVCPG4; kept for clarity
	}

	return
}
