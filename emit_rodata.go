package main

import (
	"path"
	"strings"
)

// rodataConstArgs / rodataYasmConstArgs are the constant spans of the RD
// node's two commands: the rodata.py lead (after python3) and the yasm flag
// tail between the per-platform ISA define and the output path.
var rodataConstArgs = []STR{(rodataScriptVFS).str(), argElf.str()}

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

	return build(base + ".asm"), build(base + instance.Platform.objectSuffix())
}

func emitRD(instance ModuleInstance, srcRel string, srcVFS VFS, yasmLD NodeRef, tc ModuleToolchain, emit Emitter) (NodeRef, VFS, VFS) {
	na := emit.nodeArenas()

	asmVFS, outVFS := composeRodataOutputs(instance, srcRel)
	toolName := path.Base(strings.TrimSuffix(srcRel, ".rodata"))

	pythonEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	yasmEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envYASM_TEST_SUITE, Value: strOne}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(tc.Python3), rodataConstArgs, na.strList(internStr(toolName), (srcVFS).str(), (asmVFS).str())),
			Env: pythonEnv}, Cmd{CmdArgs: na.chunkList(yasmConstHead, na.strList(argD.str(), internStr("_"+string(instance.Platform.ISA)+"_")), rodataYasmConstArgs, na.strList((outVFS).str(), (asmVFS).str())),
			Env: yasmEnv}),
		Env: yasmEnv,
		Inputs: na.inputList(na.vfsList(yasmBinaryVFS,
			rodataScriptVFS,
			srcVFS)),
		KV:               KV{P: pkRD, PC: pcLightGreen},
		Outputs:          na.vfsList(asmVFS, outVFS),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		ForeignDepRefs:   []NodeRef{yasmLD},
		Resources:        usesPython3,
	}

	return emit.emit(node), asmVFS, outVFS
}
