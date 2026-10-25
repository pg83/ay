package main

var sbomKV = KV{P: pkDX, PC: pcYellow}

const (
	sbomGenScriptRel        = "build/internal/scripts/gen_sbom.py"
	sbomConfRel             = "build/internal/conf/sbom.conf"
	clangToolchainInfoRel   = "build/internal/platform/clang_toolchain_info"
	pythonToolchainInfoRel  = "build/platform/python/ymake_python3"
	moduleLangTokenCpp      = "CPP"
	moduleLangTokenPy2      = "PY2"
	moduleLangTokenPy3      = "PY3"
	moduleLangTokenAgnostic = "AGNOSTIC"
)

func clangToolchainSbomComponent(ctx *GenCtx, platform *Platform) (*NodeRef, *VFS) {
	res := genModule(ctx, ModuleInstance{
		Path:     source(clangToolchainInfoRel),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: platform,
	})

	return res.SbomComponentRef, res.SbomComponentPath
}

func pythonToolchainSbomComponent(ctx *GenCtx, platform *Platform) (*NodeRef, *VFS) {
	res := genModule(ctx, ModuleInstance{
		Path:     source(pythonToolchainInfoRel),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: platform,
	})

	return res.SbomComponentRef, res.SbomComponentPath
}

func sbomActive(ctx *GenCtx, instance ModuleInstance) bool {
	return ctx.sbomEnabled && instance.Platform.OS == OSLinux && instance.Platform.ISA == ISAX8664 && !instance.Platform.ifEnv.bool(envAutocheck)
}

func sbomQualifies(d *ModuleData) bool {
	if d.moduleStmt.Name == tokProtoLibrary && moduleExcludesTag(d, "CPP_PROTO") {
		return false
	}

	return d.hasLicense
}

func sbomComponentLang(moduleName TOK) string {
	switch {
	case isGoModuleType(moduleName):
		return "GO"
	case moduleName == tokPrebuiltProgram:
		return moduleLangTokenAgnostic
	case moduleName == tokPy23NativeLibrary:

		return moduleLangTokenCpp
	case pyModuleTypeUsesPython3(moduleName):
		return moduleLangTokenPy3
	default:
		return moduleLangTokenCpp
	}
}

func (e *EmitContext) registeredSbomComponent(output VFS) *GeneratedFileInfo {
	info := e.codegen.lookup(output)

	if info == nil || info.ProducerRef == 0 || info.OwnerModule != e.instance.Path {
		return nil
	}

	producer := e.ctx.emit.nodes.s[info.ProducerRef]

	if producer == nil || producer.KV != &sbomKV {
		return nil
	}

	return info
}

func (e *EmitContext) emitSbomComponent(realPrjName string) (*NodeRef, *VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	moddir := instance.Path.relString()
	lang := sbomComponentLang(d.moduleStmt.Name)
	modver := d.modver

	if modver == "" {
		modver = "unknown"
	}

	out := build(moddir, "/", realPrjName, ".", lang, ".component.sbom")

	if info := e.registeredSbomComponent(out); info != nil {
		return &info.ProducerRef, &info.OutputPath
	}

	scriptVFS := source(sbomGenScriptRel)
	env := envVarsVCS

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
			wrapccPython3STR.any(),
			scriptVFS.any(),
			strOutput.any(), out.any(),
			strType.any(), strLibrary.any(),
			strPath.any(), internStr(moddir).any(),
			strVer.any(), internStr(modver).any(),
			strLang.any(), internStr(lang).any())), Env: env}),
		Env:       env,
		inputPlan: na.nodeInputs(na.inputList(na.vfsList(scriptVFS))),
		KV:        &sbomKV,
		Outputs:   na.vfsList(out),
		Resources: usesPython3,
	}

	ref := e.emitNode(node)

	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})

	return &ref, &out
}

func (e *EmitContext) emitSbomToolchainComponent(toolchainName, ver string) (*NodeRef, *VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	moddir := instance.Path.relString()

	if ver == "" {
		ver = "unknown"
	}

	out := build(moddir, "/toolchain.component.sbom")

	if info := e.registeredSbomComponent(out); info != nil {
		return &info.ProducerRef, &info.OutputPath
	}

	scriptVFS := source(sbomGenScriptRel)
	env := envVarsVCS

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
			wrapccPython3STR.any(),
			scriptVFS.any(),
			strOutput.any(), out.any(),
			strType.any(), strToolchain.any(),
			strToolchainName.any(), internStr(toolchainName).any(),
			strVer.any(), internStr(ver).any())), Env: env}),
		Env:       env,
		inputPlan: na.nodeInputs(na.inputList(na.vfsList(scriptVFS))),
		KV:        &sbomKV,
		Outputs:   na.vfsList(out),
		Resources: usesPython3,
	}

	ref := e.emitNode(node)

	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})

	return &ref, &out
}
