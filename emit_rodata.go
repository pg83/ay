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

func composeRodataOutputs(instance ModuleInstance, srcRel, objectSuffix string) (VFS, VFS) {
	base := instance.Path.relString() + "/" + srcRel

	if strings.Contains(srcRel, "/") {
		base = instance.Path.relString() + "/_/" + srcRel
	}

	return build(base, ".asm"), build(base, objectSuffix)
}

func (e *EmitContext) emitRD(srcRel string, srcVFS VFS, yasmLD NodeRef, extraInputs Closure, tc ModuleToolchain) (NodeRef, VFS, VFS) {
	instance := e.instance
	na := e.ctx.na
	asmVFS, outVFS := composeRodataOutputs(instance, srcRel, ccObjectSuffix(instance, newModuleCCInputs(&e.d.cc)))
	toolName := path.Base(strings.TrimSuffix(srcRel, ".rodata"))
	pythonEnv := envVarsVCS
	yasmEnv := envVarsVCSYasm
	extraBuckets := extraInputs.bucketList()
	inputs := na.inputs.alloc(3 + len(extraBuckets))[:0]

	inputs = append(inputs, na.vfsList(yasmBinaryVFS), na.vfsList(rodataScriptVFS), na.vfsList(srcVFS))
	inputs = append(inputs, extraBuckets...)
	na.inputs.commit(len(inputs))
	inputs = inputs[:len(inputs):len(inputs)]

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(tc.Python3), rodataConstArgs, na.anyList(internStr(toolName).any(), srcVFS.any(), asmVFS.any())),
			Env: pythonEnv}, Cmd{CmdArgs: na.chunkList(yasmConstHead, na.anyList(argD.any(), internV("_", string(instance.Platform.ISA), "_").any()), rodataYasmConstArgs, na.anyList(outVFS.any(), asmVFS.any())),
			Env: yasmEnv}),
		Env: yasmEnv, Inputs: inputs, KV: &rodataKV,
		Outputs:        na.vfsList(asmVFS, outVFS),
		ForeignDepRefs: na.refList(yasmLD),
		Resources:      usesPython3}, input: InputSpec{roots: inputs[1:]},
	}

	ref := e.emitNode(node)

	for _, output := range node.Outputs {
		e.register(GeneratedFileSpec{
			OutputPath:      output,
			ProducerRef:     ref,
			ProducerMainOut: asmVFS,
		})
	}

	return ref, asmVFS, outVFS
}

func (e *EmitContext) emitLibraryRodataSource(meta SrcMeta, in ModuleCCInputs) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := e.moduleSourceRel(src)
	srcVFS := src.vfs()

	if srcVFS == 0 {
		srcVFS = e.sourceForEmission(meta)
	}

	if env := (Environment{s: &e.frame.ifEnv}); env.bool(envARCH_AARCH64) || env.bool(envARCH_ARM) || env.bool(envARCH_PPC64LE) || env.bool(envARCH_RISCV32) || env.bool(envARCH_RISCV64) {
		e.emitRodataCpp(srcVFS, in.TC)

		return
	}

	if instance.Platform.ISA != ISAX8664 {
		ctx.onWarn(Warn{Kind: WarnUnsupportedSource, Message: fmt.Sprintf("unsupported .rodata platform %s for %q; source skipped", instance.Platform.ISA, srcRel)})

		return
	}

	yasmLDRef, _ := ctx.tool(argContribToolsYasm)
	cv := Closure{}

	if srcVFS.isBuild() {
		cv = e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainCC)
	}

	ref, _, outPath := e.emitRD(srcRel, srcVFS, yasmLDRef, cv, in.TC)

	e.collectObj(ref, outPath, meta)
}

func (e *EmitContext) emitRodataCpp(srcVFS VFS, tc ModuleToolchain) {
	na := e.ctx.na
	out := build(srcVFS.relString(), ".cpp")
	name := strings.TrimSuffix(path.Base(srcVFS.relString()), path.Ext(srcVFS.relString()))
	inputs := na.rootInputList(na.vfsList(rodataCppScriptVFS, srcVFS))

	ref := e.emitNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(tc.Python3, rodataCppScriptVFS.any(), internStr(name).any(), srcVFS.any(), out.any())), Env: envVarsVCS}),
		Env:  envVarsVCS, Inputs: inputs, Outputs: na.vfsList(out), KV: &rodataKV, Resources: usesPython3}, input: InputSpec{roots: inputs},
	})

	e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, ProducerMainOut: out})
	e.enqueueSrc(SrcMeta{Source: out.any(), Prio: stmtPrioDefault})
}
