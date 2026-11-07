package main

import (
	"path/filepath"
	"strings"
)

var (
	usesNasm       = []STR{internStr("NASM")}
	yasmBinaryPath = yasmBinaryVFS.string()
	asmConfigEnvs  = [asmConfigKeys]ENV{envAsmPrefix, envYasmFmtValue, envHardwareArch, envYasmPlatformFlagsValue, envAsmPrefixValue, envYasmPredefinedFlagsValue, envYasmFlags, envNasmFlags, envYasmPreincludesValue}
)

var yasmConstHead = []ANY{
	internStr(yasmBinaryPath).any(),
	argF.any(), argElf64.any(),
	argD.any(), argUnix.any(),
	argReplaceBB.any(),
	argReplaceSS.any(),
	argReplaceToolRootT.any(),
}

const (
	asmPrefixRaw = iota
	asmFmtRaw
	asmArchRaw
	asmPlatformFlagsRaw
	asmPrefixValueRaw
	asmPredefinedRaw
	asmYasmFlagsRaw
	asmNasmFlagsRaw
	asmPreincludesRaw
	asmConfigKeys
)

func assemblerConfigText(raw ANY, fallback string, env Environment) string {
	value := fallback

	if raw != 0 {
		value = raw.string()
	}

	return expandToken(value, env, tokenExpandFinal)
}

func assemblerRawValue(env Environment, key ENV) ANY {
	k, v := env.s.lookup(key)

	if k == envAbsent {
		return 0
	}

	env.recordResourceUse(key)

	if k == envVFS {
		return VFS(uint32(v)).any()
	}

	if v == 0 {
		return strEmpty.any()
	}

	return v.any()
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

	d.asmDefaults = [3]string{format, platformFlags, predefined}
	d.asmPending = true

	for i, key := range asmConfigEnvs {
		d.asmRaw[i] = assemblerRawValue(env, key)

		if raw := d.asmRaw[i]; raw != 0 && strHasDollar(raw) {
			d.asmPending = false
		}
	}

	if !d.asmPending {
		d.assemblerConfig(env)
	}
}

func (d *ModuleData) assemblerConfig(env Environment) {
	d.asmPending = false

	format, platformFlags, predefined := d.asmDefaults[0], d.asmDefaults[1], d.asmDefaults[2]
	raw := &d.asmRaw
	prefix := ""

	if raw[asmPrefixRaw] != 0 {
		if value := raw[asmPrefixRaw].string(); value != "" {
			prefix = "--prefix=" + value
		}
	}

	d.asmFormat = internV(assemblerConfigText(raw[asmFmtRaw], format, env), assemblerConfigText(raw[asmArchRaw], "64", env))
	d.yasmPlatformFlags = appendQuotedWords(d.yasmPlatformFlags[:0], assemblerConfigText(raw[asmPlatformFlagsRaw], platformFlags, env), 0)

	d.yasmFlags = appendQuotedWords(d.yasmFlags[:0], assemblerConfigText(raw[asmPrefixValueRaw], prefix, env)+" "+
		assemblerConfigText(raw[asmPredefinedRaw], predefined, env)+" "+assemblerConfigText(raw[asmYasmFlagsRaw], "", env), 0)

	d.nasmFlags = appendQuotedWords(d.nasmFlags[:0], assemblerConfigText(raw[asmNasmFlagsRaw], "", env), 0)
	d.asmPreincludes = internAnys(strings.Fields(assemblerConfigText(raw[asmPreincludesRaw], "", env)))
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

	if e.d.asmPending {
		e.d.assemblerConfig(Environment{s: &e.frame.ifEnv})
	}

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
