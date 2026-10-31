package main

import (
	"path/filepath"
	"strings"
)

var (
	usesNasm       = []STR{internStr("NASM")}
	yasmBinaryPath = yasmBinaryVFS.string()
)

var yasmConstHead = []ANY{
	internStr(yasmBinaryPath).any(),
	argF.any(), argElf64.any(),
	argD.any(), argUnix.any(),
	argReplaceBB.any(),
	argReplaceSS.any(),
	argReplaceToolRootT.any(),
}

func assemblerConfigValue(env Environment, key ENV, fallback string) string {
	value := fallback

	if k, v := env.s.lookup(key); k != envAbsent {
		env.recordResourceUse(key)

		if k == envVFS {
			value = VFS(uint32(v)).string()
		} else {
			value = v.string()
		}
	}

	return expandToken(value, env, tokenExpandFinal)
}

func (d *ModuleData) collectAssemblerConfig(env Environment) {
	format, platformFlags, predefined := "elf", "-D UNIX", "-g dwarf2"

	switch {
	case env.bool(envOS_DARWIN) || env.bool(envOsIos):
		format, platformFlags, predefined = "macho", "-D DARWIN -D UNIX", ""
	case env.bool(envOS_WINDOWS) && env.bool(envARCH_X86_64):
		format, platformFlags, predefined = "win", "-D WIN64", ""
	case env.bool(envOS_WINDOWS) && env.bool(envARCH_I386):
		format, platformFlags, predefined = "win", "-D WIN32", ""
	}

	prefix := ""

	if value := env.string(envAsmPrefix); value != "" {
		prefix = "--prefix=" + value
	}

	d.asmFormat = internV(assemblerConfigValue(env, envYasmFmtValue, format), assemblerConfigValue(env, envHardwareArch, "64"))
	d.yasmPlatformFlags = appendQuotedWords(d.yasmPlatformFlags[:0], assemblerConfigValue(env, envYasmPlatformFlagsValue, platformFlags), 0)

	d.yasmFlags = appendQuotedWords(d.yasmFlags[:0], assemblerConfigValue(env, envAsmPrefixValue, prefix)+" "+
		assemblerConfigValue(env, envYasmPredefinedFlagsValue, predefined)+" "+assemblerConfigValue(env, envYasmFlags, ""), 0)

	d.nasmFlags = appendQuotedWords(d.nasmFlags[:0], assemblerConfigValue(env, envNasmFlags, ""), 0)
	d.asmPreincludes = internAnys(strings.Fields(assemblerConfigValue(env, envYasmPreincludesValue, "")))
}

func (e *EmitContext) asmOutputFor(srcVFS VFS) VFS {
	instance := e.instance
	suffix := ccObjectSuffix(instance, newModuleCCInputs(&e.d.cc))
	rel := composeSrcDirOutputRel(instance.Path.relString(), srcVFS.relString())

	return build(instance.Path.relString(), "/", strings.TrimSuffix(rel, filepath.Ext(rel)), suffix)
}

func (e *EmitContext) emitASAsm(srcVFS VFS, in ModuleCCInputs, yasmLD NodeRef, yasmBin VFS, nasm bool, reserved NodeRef) (NodeRef, VFS) {
	instance := e.instance
	na := e.ctx.na
	outVFS := e.asmOutputFor(srcVFS)
	inVFS := srcVFS
	preincludes := e.d.asmPreincludes
	sourceFlags := in.PerSourceCFlags
	roots := na.vfs.alloc(1 + len(preincludes))[:0]

	roots = append(roots, inVFS)

	if !nasm {
		for _, preinclude := range preincludes {
			roots = append(roots, e.moduleSourceVFS(preinclude))
		}
	}

	na.vfs.commit(len(roots))
	roots = roots[:len(roots):len(roots)]

	cmdArgs := na.anys.alloc(20 + len(e.d.yasmPlatformFlags) + len(e.d.yasmFlags) + len(e.d.nasmFlags) +
		2*len(in.AddIncl) + len(sourceFlags) + 2*len(preincludes))[:0]

	if nasm {
		cmdArgs = append(cmdArgs, nasmBinaryVFS.any(), argF.any(), e.d.asmFormat.any(),
			argD.any(), internV("_", string(instance.Platform.ISA), "_").any())

		cmdArgs = append(cmdArgs, e.d.nasmFlags...)
	} else {
		cmdArgs = append(cmdArgs, yasmBin.any(), argF.any(), e.d.asmFormat.any())
		cmdArgs = append(cmdArgs, e.d.yasmPlatformFlags...)

		if instance.Platform.YasmDebugInfo {
			cmdArgs = append(cmdArgs, argReplaceBB.any(), argReplaceSS.any(), argReplaceToolRootT.any())
		}

		cmdArgs = append(cmdArgs,
			argD.any(), internV("_", string(instance.Platform.ISA), "_").any(),
			argDYasm.any(),
		)

		cmdArgs = append(cmdArgs, e.d.yasmFlags...)
	}

	cmdArgs = append(cmdArgs,
		argI.any(), argB.any(),
		argI.any(), argS.any(),
	)

	for _, p := range in.AddIncl {
		cmdArgs = append(cmdArgs, argI.any(), p.any())
	}

	cmdArgs = append(cmdArgs, sourceFlags...)
	cmdArgs = append(cmdArgs, argDashO.any(), outVFS.any())

	for _, preinclude := range preincludes {
		cmdArgs = append(cmdArgs, strP.any(), preinclude)
	}

	cmdArgs = append(cmdArgs, inVFS.any())

	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	env := envVarsVCS

	if !nasm && instance.Platform.YasmDebugInfo {
		env = envVarsVCSYasm
	}

	buildFrom := na.inputList(roots)
	inputs := buildFrom

	if !nasm {
		inputs = na.inputList(na.vfsList(yasmBin), roots)
	}

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env: env, Inputs: inputs, Outputs: na.vfsList(outVFS),
		KV: &asKV}, input: InputSpec{roots: buildFrom},
	}

	if nasm {
		node.Resources = usesNasm
	} else {
		node.ForeignDepRefs = na.refList(yasmLD)
	}

	if reserved != 0 {
		e.emitReservedNode(node, reserved)

		return reserved, outVFS
	}

	return e.emitNode(node), outVFS
}

func (e *EmitContext) emitLibraryYasmSource(meta SrcMeta, in ModuleCCInputs) {
	src := meta.Source
	d := e.d
	srcVFS := src.vfs()

	if srcVFS == 0 {
		srcVFS = e.sourceForEmission(meta)
	}

	asIn := in

	asIn.AddIncl = d.asmAddIncl
	asIn.IncludeView = e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainAsm)

	nasm := d.useNasm && filepath.Ext(srcVFS.relString()) == ".asm"

	var yasmLD NodeRef
	var yasmBin VFS

	if !nasm {
		yasmLD, yasmBin = e.tool(argContribToolsYasm)
	}

	ref, outPath := e.emitASAsm(srcVFS, asIn, yasmLD, yasmBin, nasm, meta.CompileRef)

	e.collectObj(ref, outPath, meta)
}
