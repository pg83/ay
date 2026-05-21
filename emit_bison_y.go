package main

import (
	"path/filepath"
	"strings"
)

func emitBisonY(ctx *genCtx, instance ModuleInstance, srcRel string, in ModuleCCInputs, genExt string) *sourceEmit {
	bisonRef, bisonBin := bisonTool(ctx, instance)
	m4Ref, m4Bin := m4Tool(ctx, instance)

	baseNoExt := strings.TrimSuffix(srcRel, filepath.Ext(srcRel))
	headerRel := baseNoExt + ".h"
	generatedRel := "_/" + srcRel + genExt
	headerVFS := Build(instance.Path + "/" + headerRel)
	generatedVFS := Build(instance.Path + "/" + generatedRel)
	srcVFS := Source(instance.Path + "/" + srcRel)
	headerParsed := []includeDirective{{kind: includeQuoted, target: srcVFS.Rel}}
	if scanner := ctx.scannerFor(instance); scanner != nil {
		headerParsed = append(headerParsed, scanner.parsers.sourceParsedBuckets(srcVFS.Rel).bucket(parsedIncludesLocal)...)
	}
	registerGeneratedParsedOutput(ctx, instance, "YC", headerVFS, headerParsed)
	registerGeneratedParsedOutput(ctx, instance, "YC", generatedVFS, []includeDirective{
		{kind: includeQuoted, target: headerVFS.Rel},
	})

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"BISON_PKGDATADIR":       "$(S)/contrib/tools/bison/data",
		"M4":                     m4Bin,
	}

	ycRef := ctx.emit.Emit(&Node{
		Cmds: []Cmd{
			{
				CmdArgs: []string{
					bisonBin,
					"-v",
					"--defines=" + headerVFS.String(),
					"-o",
					generatedVFS.String(),
					srcVFS.String(),
				},
				Env: env,
			},
		},
		DepRefs: []NodeRef{bisonRef, m4Ref},
		Env:     env,
		Inputs:  []VFS{Build("contrib/tools/bison/bison"), Build("contrib/tools/m4/m4"), srcVFS},
		Outputs: []VFS{headerVFS, generatedVFS},
		KV: map[string]string{
			"p":  "YC",
			"pc": "light-green",
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
	})
	bindGeneratedOutput(ctx, instance, headerVFS, ycRef)
	bindGeneratedOutput(ctx, instance, generatedVFS, ycRef)

	ccIn := in
	ccIn.ExtraDepRefs = []NodeRef{ycRef}
	ccIn.IncludeInputs = walkClosure(ctx, instance, generatedVFS, in)

	ccRef, ccOut := EmitCC(instance, generatedRel, generatedVFS, ccIn, ctx.host, ctx.emit)
	ccInputs := append([]VFS{srcVFS}, ccIn.IncludeInputs...)

	return &sourceEmit{
		Ref:          ccRef,
		OutPath:      ccOut,
		CcIns:        ccInputs,
		PrimaryCount: 1,
	}
}

func bisonTool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool("contrib/tools/bison")
	return ref, bin.String()
}

func m4Tool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool("contrib/tools/m4")
	return ref, bin.String()
}
