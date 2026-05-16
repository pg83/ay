package main

import (
	"path/filepath"
	"strings"
)

func emitBisonY(ctx *genCtx, instance ModuleInstance, srcRel string, in ModuleCCInputs, genExt string) *sourceEmit {
	if genExt == "" {
		genExt = ".cpp"
	}

	bisonRef, bisonBin := bisonTool(ctx, instance)
	m4Ref, m4Bin := m4Tool(ctx, instance)

	baseNoExt := strings.TrimSuffix(srcRel, filepath.Ext(srcRel))
	headerRel := baseNoExt + ".h"
	generatedRel := "_/" + srcRel + genExt
	headerVFS := Build(instance.Path + "/" + headerRel)
	generatedVFS := Build(instance.Path + "/" + generatedRel)
	srcVFS := Source(instance.Path + "/" + srcRel)

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
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
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

	ccIn := in
	ccIn.IsGenerated = true
	ccIn.HasGenerator = true
	ccIn.Generator = ycRef
	ccIn.IncludeInputs = append([]VFS{headerVFS}, walkClosure(ctx, instance, srcVFS, in)...)

	ccRef, ccOut := EmitCC(instance, generatedRel, ccIn, ctx.host, ctx.emit)
	ccInputs := append([]VFS{generatedVFS}, ccIn.IncludeInputs...)

	return &sourceEmit{
		Ref:          ccRef,
		OutPath:      ccOut,
		CcIns:        ccInputs,
		PrimaryCount: 2,
	}
}

func bisonTool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	const p = "contrib/tools/bison"

	tool := NewToolInstance(ctx.host, p)
	tool.Flags = inferFlagsFromPath(p, true)
	res := genModule(ctx, tool)
	if res.LDPath != "" {
		return res.LDRef, res.LDPath
	}

	return res.LDRef, Build("contrib/tools/bison/bison").String()
}

func m4Tool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	const p = "contrib/tools/m4"

	tool := NewToolInstance(ctx.host, p)
	tool.Flags = inferFlagsFromPath(p, true)
	res := genModule(ctx, tool)
	if res.LDPath != "" {
		return res.LDRef, res.LDPath
	}

	return res.LDRef, Build("contrib/tools/m4/m4").String()
}
