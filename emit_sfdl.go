package main

import (
	"path/filepath"
	"strings"
)

var sfKV = KV{P: pkSF, PC: pcYellow}

func (e *EmitContext) emitLibrarySfdlSource(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	srcRel := src.string()
	toolRef, toolBin := ctx.tool(argToolsCalcstaticopt)
	srcVFS := source(instance.Path.rel(), "/", srcRel)
	tmpVFS := build(instance.Path.rel(), "/", srcRel, ".tmp")
	incVFS := build(instance.Path.rel(), "/", strings.TrimSuffix(srcRel, filepath.Ext(srcRel)))
	plainEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	toolEnv := instance.Platform.ToolEnvVars
	blocks := d.cc.CCBlocks

	cmd0 := Cmd{CmdArgs: na.chunkList(
		blocks.cxxHead,
		instance.Platform.CCHead,
		blocks.flags,
		blocks.cxxTail,
		na.strList(
			strE,
			strC3,
			strX,
			strC4,
			strQunusedArguments,
			argDashO.str(),
			tmpVFS.str(),
			srcVFS.str(),
		),
	), Env: plainEnv}

	cmd1 := Cmd{CmdArgs: na.chunkList(na.strList(
		toolBin.str(),
		strI2,
		tmpVFS.str(),
		strA,
		strS,
	)), Env: toolEnv, Stdout: incVFS.str()}

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
