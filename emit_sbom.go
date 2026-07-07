package main

var sbomKV = KV{P: pkDX, PC: pcYellow}

const (
	sbomGenScriptRel        = "build/internal/scripts/gen_sbom.py"
	sbomConfRel             = "build/internal/conf/sbom.conf"
	clangToolchainInfoRel   = "build/internal/platform/clang_toolchain_info"
	pythonToolchainInfoRel  = "build/platform/python/ymake_python3"
	moduleLangTokenCpp      = "CPP"
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
	return ctx.sbomEnabled && instance.Platform.OS == OSLinux && instance.Platform.ISA == ISAX8664
}

func sbomQualifies(d *ModuleData) bool {
	if d.moduleStmt.Name == tokProtoLibrary && moduleExcludesTag(d, "CPP_PROTO") {
		return false
	}

	return d.hasLicense
}

func sbomComponentLang(moduleName TOK) string {
	switch {
	case moduleName == tokGoLibrary || moduleName == tokGoProgram:
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
	scriptVFS := source(sbomGenScriptRel)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkListSTR([]STR{
			wrapccPython3STR,
			scriptVFS.fullSTR(),
			strOutput, out.fullSTR(),
			strType, strLibrary,
			strPath, internStr(moddir),
			strVer, internStr(modver),
			strLang, internStr(lang),
		}), Env: env}),
		Env:          env,
		Inputs:       na.inputList([]VFS{scriptVFS}),
		KV:           &sbomKV,
		Outputs:      na.vfsList(out),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	}

	ref := ctx.emit.emitNode(node)

	return &ref, &out
}

func emitSbomToolchainComponent(ctx *GenCtx, instance ModuleInstance, toolchainName, ver string) (*NodeRef, *VFS) {
	na := ctx.na
	moddir := instance.Path.relString()

	if ver == "" {
		ver = "unknown"
	}

	out := build(moddir, "/toolchain.component.sbom")
	scriptVFS := source(sbomGenScriptRel)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkListSTR([]STR{
			wrapccPython3STR,
			scriptVFS.fullSTR(),
			strOutput, out.fullSTR(),
			strType, strToolchain,
			strToolchainName, internStr(toolchainName),
			strVer, internStr(ver),
		}), Env: env}),
		Env:          env,
		Inputs:       na.inputList([]VFS{scriptVFS}),
		KV:           &sbomKV,
		Outputs:      na.vfsList(out),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	}

	ref := ctx.emit.emitNode(node)

	return &ref, &out
}
