package main

import (
	"path/filepath"
	"strings"
)

var (
	antlr4RuntimeHeaderPath = antlr4RuntimeHeaderVFS.String()
)

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

		base := strings.TrimSuffix(filepath.Base(srcCpp.Rel()), ".cpp")
		g4CppPath := Build(instance.Path + "/" + base + ".g4.cpp")
		g4CppRel := base + ".g4.cpp"

		if reg != nil {
			emits := make([]includeDirective, 0, 1+len(outputIncludes))
			emits = append(emits, includeDirective{kind: includeQuoted, target: internStr(antlr4RuntimeHeaderVFS.Rel())})

			for _, h := range outputIncludes {
				emits = append(emits, includeDirective{kind: includeQuoted, target: internStr(h)})
			}

			registerGeneratedParsedOutput(ctx, instance, "CP", g4CppPath, emits, nil)
		}

		ccIn := in
		ccIn.ExtraDepRefs = nil
		closure := walkClosure(ctx, instance, g4CppPath, ccIn)

		cpInputs := make([]VFS, 0, 2+len(jvInputs)+len(closure)+2)
		cpInputs = append(cpInputs, jvPrimary)

		if srcCpp != jvPrimary {
			cpInputs = append(cpInputs, srcCpp)
		}

		cpInputs = append(cpInputs, ctx.scripts[antlr4FsToolsVFS]...)
		cpInputs = append(cpInputs, jvInputs...)
		cpInputs = append(cpInputs, closure...)

		cpRef := EmitJVCPG4(instance, srcCpp, g4CppPath, jvRef, jvPrimary, jvInputs, closure, ctx.scripts, ctx.emit)

		ccIncludeInputs := make([]VFS, 0, 3+len(jvInputs)+len(closure)+2)
		ccIncludeInputs = append(ccIncludeInputs, jvPrimary)
		ccIncludeInputs = append(ccIncludeInputs, srcH)
		ccIncludeInputs = append(ccIncludeInputs, ctx.scripts[antlr4FsToolsVFS]...)
		ccIncludeInputs = append(ccIncludeInputs, jvInputs...)
		ccIncludeInputs = append(ccIncludeInputs, closure...)

		ccIn.IncludeInputs = ccIncludeInputs
		ccIn.ExtraDepRefs = []NodeRef{jvRef, cpRef}
		ccIn.PerSourceCFlags = []ARG{argWnoUnusedVariable}
		ccRef, ccOut, _ := EmitCC(instance, g4CppRel, g4CppPath, ccIn, ctx.host, ctx.emit)

		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)
		_ = cpInputs
	}

	return
}
