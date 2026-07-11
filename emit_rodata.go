package main

import (
	"fmt"
	"path"
	"strings"
)

var (
	rodataConstArgs = []ANY{rodataScriptVFS.any(), argElf.any()}
	rodataKV        = KV{P: pkRD, PC: pcLightGreen}
)

var rodataYasmConstArgs = []ANY{
	argDYasm.any(),
	argDashG.any(), argDwarf2.any(),
	argI.any(), argB.any(),
	argI.any(), argS.any(),
	argDashO.any(),
}

func composeRodataOutputs(instance ModuleInstance, srcRel string) (VFS, VFS) {
	base := instance.Path.relString() + "/" + srcRel

	if strings.Contains(srcRel, "/") {
		base = instance.Path.relString() + "/_/" + srcRel
	}

	return build(base, ".asm"), build(base, instance.Platform.objectSuffix())
}

func (e *EmitContext) emitRD(srcRel string, srcVFS VFS, yasmLD NodeRef, extraInputs Closure, extraDepRefs []NodeRef, tc ModuleToolchain) (NodeRef, VFS, VFS) {
	instance := e.instance
	na := e.ctx.na
	asmVFS, outVFS := composeRodataOutputs(instance, srcRel)
	toolName := path.Base(strings.TrimSuffix(srcRel, ".rodata"))
	pythonEnv := envVarsVCS
	yasmEnv := envVarsVCSYasm

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(tc.Python3.any()), rodataConstArgs, na.anyList(internStr(toolName).any(), srcVFS.any(), asmVFS.any())),
			Env: pythonEnv}, Cmd{CmdArgs: na.chunkList(yasmConstHead, na.anyList(argD.any(), internV("_", string(instance.Platform.ISA), "_").any()), rodataYasmConstArgs, na.anyList(outVFS.any(), asmVFS.any())),
			Env: yasmEnv}),
		Env: yasmEnv,
		Inputs: na.inputList(na.vfsList(yasmBinaryVFS,
			rodataScriptVFS,
			srcVFS), extraInputs.buckets...),
		KV:             &rodataKV,
		Outputs:        na.vfsList(asmVFS, outVFS),
		ForeignDepRefs: na.refList(yasmLD),
		Resources:      usesPython3,
	}

	if len(extraDepRefs) > 0 {
		node.DepRefs = na.noderefs.list(extraDepRefs...)
	}

	return e.emitNode(node), asmVFS, outVFS
}

func (e *EmitContext) emitLibraryRodataSource(meta SrcMeta, in ModuleCCInputs) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := e.moduleSourceRel(src)
	srcVFS := src.vfs()

	if instance.Platform.ISA != ISAX8664 {
		ctx.onWarn(Warn{Kind: WarnUnsupportedSource, Message: fmt.Sprintf("unsupported .rodata platform %s for %q; source skipped", instance.Platform.ISA, srcRel)})

		return
	}

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	}

	yasmLDRef, _ := ctx.tool(argContribToolsYasm)
	cv := Closure{}

	if srcVFS.isBuild() {
		cv = e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainCC)
	}

	ref, _, outPath := e.emitRD(srcRel, srcVFS, yasmLDRef, cv, nil, in.TC)

	e.collectObj(ref, outPath, meta)
}
