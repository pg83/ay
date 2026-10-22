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

func assemblerConfigValue(env Environment, name, fallback string) string {
	value, bound := env.lookup(name)

	if !bound {
		value = fallback
	}

	return expandToken(value, env, tokenExpandFinal)
}

func (d *ModuleData) collectAssemblerConfig(env Environment) {
	format, platformFlags, predefined := "elf", "-D UNIX", "-g dwarf2"

	switch {
	case env.bool(envOS_DARWIN) || env.bool(internEnv("OS_IOS")):
		format, platformFlags, predefined = "macho", "-D DARWIN -D UNIX", ""
	case env.bool(envOS_WINDOWS) && env.bool(envARCH_X86_64):
		format, platformFlags, predefined = "win", "-D WIN64", ""
	case env.bool(envOS_WINDOWS) && env.bool(envARCH_I386):
		format, platformFlags, predefined = "win", "-D WIN32", ""
	}

	prefix := ""

	if value := env.string(internEnv("ASM_PREFIX")); value != "" {
		prefix = "--prefix=" + value
	}

	d.asmFormat = internStr(assemblerConfigValue(env, "_YASM_FMT_VALUE", format) + assemblerConfigValue(env, "HARDWARE_ARCH", "64"))
	d.yasmPlatformFlags = internAnys(splitShellWords(assemblerConfigValue(env, "_YASM_PLATFORM_FLAGS_VALUE", platformFlags)))

	d.yasmFlags = internAnys(splitShellWords(assemblerConfigValue(env, "ASM_PREFIX_VALUE", prefix) + " " +
		assemblerConfigValue(env, "_YASM_PREDEFINED_FLAGS_VALUE", predefined) + " " + assemblerConfigValue(env, "YASM_FLAGS", "")))

	d.nasmFlags = internAnys(splitShellWords(assemblerConfigValue(env, "NASM_FLAGS", "")))
	d.asmPreincludes = internAnys(strings.Fields(assemblerConfigValue(env, "YASM_PREINCLUDES_VALUE", "")))
}

func (e *EmitContext) emitASAsm(srcVFS VFS, in ModuleCCInputs, yasmLD NodeRef, nasm bool) (NodeRef, VFS) {
	instance := e.instance
	na := e.ctx.na
	suffix := ".o"

	if instance.Platform.PIC {
		suffix = ".pic.o"
	}

	rel := composeSrcDirOutputRel(instance.Path.relString(), srcVFS.relString())
	outVFS := build(instance.Path.relString(), "/", strings.TrimSuffix(rel, filepath.Ext(rel)), suffix)
	inVFS := srcVFS
	preincludes := e.d.asmPreincludes
	sourceFlags := in.PerSourceCFlags

	for i, flag := range sourceFlags {
		if flag.string() == "PREINCLUDES" {
			preincludes = nil

			for _, include := range sourceFlags[i+1:] {
				if include.string() != "PREINCLUDES" {
					preincludes = append(preincludes, include)
				}
			}

			preincludes = append(preincludes, e.d.asmPreincludes...)
			sourceFlags = sourceFlags[:i]

			break
		}
	}

	closures := []Closure{in.IncludeView}

	if !nasm {
		for _, preinclude := range preincludes {
			input := e.moduleSourceVFS(preinclude)

			closures = append(closures, e.scanner.assemblerContext(e.d.scanCtx).walkClosure(input, e.d.scanCtx, scanDomainAsm))
		}
	}

	cmdArgs := na.anys.alloc(20 + len(e.d.yasmPlatformFlags) + len(e.d.yasmFlags) + len(e.d.nasmFlags) +
		2*len(in.AddIncl) + len(sourceFlags) + 2*len(preincludes))[:0]

	if nasm {
		cmdArgs = append(cmdArgs, nasmBinaryVFS.any(), argF.any(), e.d.asmFormat.any(),
			argD.any(), internV("_", string(instance.Platform.ISA), "_").any())

		cmdArgs = append(cmdArgs, e.d.nasmFlags...)
	} else {
		cmdArgs = append(cmdArgs, yasmBinaryVFS.any(), argF.any(), e.d.asmFormat.any())
		cmdArgs = append(cmdArgs, e.d.yasmPlatformFlags...)
		cmdArgs = append(cmdArgs, argReplaceBB.any(), argReplaceSS.any(), argReplaceToolRootT.any())

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
		cmdArgs = append(cmdArgs, internStr("-P").any(), preinclude)
	}

	cmdArgs = append(cmdArgs, inVFS.any())

	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	env := envVarsVCS

	if !nasm {
		env = envVarsVCSYasm
	}

	inputs := na.dedupClosureChunks(closures...)

	if !nasm {
		inputs = na.inputList(na.vfsList(yasmBinaryVFS), inputs...)
	}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       inputs,
		InputClosure: in.IncludeView,
		Outputs:      na.vfsList(outVFS),
		KV:           &asKV,
	}

	if nasm {
		node.Resources = usesNasm
	} else {
		node.ForeignDepRefs = na.refList(yasmLD)
	}

	return e.emitNode(node), outVFS
}

func (e *EmitContext) emitLibraryYasmSource(meta SrcMeta, in ModuleCCInputs) {
	src := meta.Source
	ctx, d := e.ctx, e.d
	srcVFS := src.vfs()

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	}

	asIn := in

	asIn.AddIncl = d.asmAddIncl
	asIn.IncludeView = e.scanner.assemblerContext(d.scanCtx).walkClosure(srcVFS, d.scanCtx, scanDomainAsm)

	nasm := d.useNasm && filepath.Ext(srcVFS.relString()) == ".asm"

	var yasmLD NodeRef

	if !nasm {
		yasmLD, _ = ctx.tool(argContribToolsYasm)
	}

	ref, outPath := e.emitASAsm(srcVFS, asIn, yasmLD, nasm)

	e.collectObj(ref, outPath, meta)
}
