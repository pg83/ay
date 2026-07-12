package main

import (
	"strings"
)

var yasmBinaryPath = yasmBinaryVFS.string()

var yasmConstHead = []ANY{
	internStr(yasmBinaryPath).any(),
	argF.any(), argElf64.any(),
	argD.any(), argUnix.any(),
	argReplaceBB.any(),
	argReplaceSS.any(),
	argReplaceToolRootT.any(),
}

func (e *EmitContext) emitASYasm(srcRel string, srcVFS VFS, in ModuleCCInputs, yasmLD NodeRef) (NodeRef, VFS) {
	instance := e.instance
	na := e.ctx.na
	stem := strings.TrimSuffix(srcRel, ".asm")
	suffix := ".o"

	if instance.Platform.PIC {
		suffix = ".pic.o"
	}

	var outVFS VFS

	if strings.Contains(srcRel, "/") {
		outVFS = build(instance.Path.relString(), "/_/", stem, suffix)
	} else {
		outVFS = build(instance.Path.relString(), "/", stem, suffix)
	}

	inVFS := srcVFS

	var predefinedFlags []string

	if !asmlibYasmModules[instance.Path.relString()] {
		predefinedFlags = []string{"-g", "dwarf2"}
	}

	cmdArgs := na.anys.alloc(len(yasmConstHead) + 10 + len(predefinedFlags) + 2*len(in.AddIncl))[:0]

	cmdArgs = append(cmdArgs, yasmConstHead...)

	cmdArgs = append(cmdArgs,
		argD.any(), internV("_", string(instance.Platform.ISA), "_").any(),
		argDYasm.any(),
	)

	cmdArgs = appendInternAnys(cmdArgs, predefinedFlags)

	cmdArgs = append(cmdArgs,
		argI.any(), argB.any(),
		argI.any(), argS.any(),
	)

	for _, p := range in.AddIncl {
		cmdArgs = append(cmdArgs, argI.any(), p.any())
	}

	cmdArgs = append(cmdArgs,
		argDashO.any(), outVFS.any(),
		inVFS.any(),
	)

	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	env := envVarsVCSYasm
	inputs := na.inputs.alloc(2 + len(in.IncludeView.buckets))[:0]

	inputs = append(inputs, na.vfsList(yasmBinaryVFS), na.vfsList(in.IncludeView.self))
	inputs = append(inputs, in.IncludeView.buckets...)
	na.inputs.commit(len(inputs))
	inputs = inputs[:len(inputs):len(inputs)]

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:     env,
		Inputs:  inputs,
		Outputs: na.vfsList(outVFS),
		KV:      &asKV,
	}

	node.ForeignDepRefs = na.refList(yasmLD)

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = in.ExtraDepRefs
	}

	return e.emitNode(node), outVFS
}

func (e *EmitContext) emitLibraryYasmSource(meta SrcMeta, in ModuleCCInputs) {
	src := meta.Source
	ctx, d := e.ctx, e.d
	srcVFS := src.vfs()
	srcRel := e.moduleSourceRel(src)

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	}

	asIn := in
	scanIn := in

	if len(d.asmAddIncl) > 0 {
		scanIn.AddIncl = dedup(in.AddIncl, d.asmAddIncl)
		asIn.AddIncl = scanIn.AddIncl
	}

	asIn.IncludeView = e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainAsm)

	yasmLD, _ := ctx.tool(argContribToolsYasm)
	ref, outPath := e.emitASYasm(srcRel, srcVFS, asIn, yasmLD)

	e.collectObj(ref, outPath, meta)
}
