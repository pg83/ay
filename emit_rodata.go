package main

import (
	"path"
	"strings"
)

var (
	rodataConstArgs = []STR{(rodataScriptVFS).str(), argElf.str()}
	rodataKV        = KV{P: pkRD, PC: pcLightGreen}
)

var rodataYasmConstArgs = []STR{
	argDYasm.str(),
	argDashG.str(), argDwarf2.str(),
	argI.str(), argB.str(),
	argI.str(), argS.str(),
	argDashO.str(),
}

func composeRodataOutputs(instance ModuleInstance, srcRel string) (VFS, VFS) {
	base := instance.Path.rel() + "/" + srcRel

	if strings.Contains(srcRel, "/") {
		base = instance.Path.rel() + "/_/" + srcRel
	}

	return build(base, ".asm"), build(base, instance.Platform.objectSuffix())
}

func emitRD(instance ModuleInstance, srcRel string, srcVFS VFS, yasmLD NodeRef, extraInputs []VFS, extraDepRefs []NodeRef, tc ModuleToolchain, emit *StreamingEmitter) (NodeRef, VFS, VFS) {
	na := emit.nodeArenas()
	asmVFS, outVFS := composeRodataOutputs(instance, srcRel)
	toolName := path.Base(strings.TrimSuffix(srcRel, ".rodata"))
	pythonEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	yasmEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envYASM_TEST_SUITE, Value: strOne}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(tc.Python3), rodataConstArgs, na.strList(internStr(toolName), (srcVFS).str(), (asmVFS).str())),
			Env: pythonEnv}, Cmd{CmdArgs: na.chunkList(yasmConstHead, na.strList(argD.str(), internV("_", string(instance.Platform.ISA), "_")), rodataYasmConstArgs, na.strList((outVFS).str(), (asmVFS).str())),
			Env: yasmEnv}),
		Env: yasmEnv,
		Inputs: na.inputList(na.vfsList(yasmBinaryVFS,
			rodataScriptVFS,
			srcVFS), extraInputs),
		KV:             &rodataKV,
		Outputs:        na.vfsList(asmVFS, outVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: []NodeRef{yasmLD},
		Resources:      usesPython3,
	}

	if len(extraDepRefs) > 0 {
		node.DepRefs = extraDepRefs
	}

	return emit.emit(node), asmVFS, outVFS
}

func (e *EmitContext) emitLibraryRodataSource(src STR) *SourceEmit {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()

	if instance.Platform.ISA != ISAX8664 {
		throwFmt("gen: unsupported .rodata platform %s for %q", instance.Platform.ISA, srcRel)
	}

	yasmLDRef, _ := ctx.tool(argContribToolsYasm)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	in := e.ccInputsFor(srcVFS)
	ref, _, outPath := emitRD(instance, srcRel, srcVFS, yasmLDRef, nil, nil, in.TC, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
