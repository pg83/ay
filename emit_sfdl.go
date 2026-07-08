package main

import (
	"path/filepath"
	"strings"
)

var sfKV = KV{P: pkSF, PC: pcYellow}

func (e *EmitContext) emitLibrarySfdlSource(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	srcRel := src.string()
	toolRef, toolBin := ctx.tool(argToolsCalcstaticopt)
	srcVFS := source(instance.Path.relString(), "/", srcRel)
	tmpVFS := build(instance.Path.relString(), "/", srcRel, ".tmp")
	incVFS := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, filepath.Ext(srcRel)))
	plainEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}
	toolEnv := instance.Platform.ToolEnvVars
	blocks := d.cc.CCBlocks

	cmd0 := Cmd{CmdArgs: na.chunkList(
		blocks.cxxHead,
		instance.Platform.CCHead,
		blocks.flags,
		blocks.cxxTail,
		na.anyList(
			strE.any(),
			strC3.any(),
			strX.any(),
			strC4.any(),
			strQunusedArguments.any(),
			argDashO.any(),
			tmpVFS.any(),
			srcVFS.any(),
		),
	), Env: plainEnv}

	cmd1 := Cmd{CmdArgs: na.chunkList(na.anyList(
		toolBin.any(),
		strI2.any(),
		tmpVFS.any(),
		strA.any(),
		strS.any(),
	)), Env: toolEnv, Stdout: incVFS}

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd0, cmd1),
		Env:            toolEnv,
		Inputs:         na.inputList(na.vfsList(toolBin, srcVFS)),
		KV:             &sfKV,
		Outputs:        na.vfsList(tmpVFS, incVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(toolRef),
		Resources:      instance.Platform.CCUsesResources,
	}

	ref := ctx.emit.emitNode(node)

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    tmpVFS,
		ProducerRef:   ref,
		GeneratorRefs: []NodeRef{toolRef},
	})
	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    incVFS,
		ProducerRef:   ref,
		GeneratorRefs: []NodeRef{toolRef},
		ClosureLeaves: []VFS{tmpVFS, srcVFS},
	})
}
