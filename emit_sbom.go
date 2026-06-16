package main

// SBOM (Software Bill of Materials) component generation. The internal build
// attaches _GEN_SBOM_COMPONENT to every module that declares a LICENSE():
// LICENSE() calls _CONTRIB_MODULE_HOOKS (build/conf/license.conf:31), whose
// plugin (build/internal/plugins/contrib_hooks.py) runs gen_sbom.py — under
// _NEED_SBOM_INFO (linux x86_64) and non-JAVA — to produce a
// <REALPRJNAME>.<LANG>.component.sbom global output. That output propagates up
// the link closure; only programs built RELEASE collect it (sbom.conf:26), so a
// debug target (ya-bin) links a licensed lib without pulling its component.
// Emitting one per licensed module and letting the closure + collection gate
// prune is faithful to upstream (which also generates unconditionally).

const sbomGenScriptRel = "build/internal/scripts/gen_sbom.py"

// sbomConfRel is the config file that turns the SBOM feature on
// (SBOM_GENERATION_ALLOWED=yes); present only in the internal contour.
const sbomConfRel = "build/internal/conf/sbom.conf"

// clangToolchainInfoRel is the RESOURCES_LIBRARY that TOOLCHAIN(clang) tags; it
// is _SRC_CPP_TOOLCHAIN_INFO_PEER (sbom.conf:9), the PEERDIR every C-family
// _SRC(cpp|cxx|cc|C|c|m) carries (ymake.core.conf:3438-3483).
const clangToolchainInfoRel = "build/internal/platform/clang_toolchain_info"

// clangToolchainSbomComponent resolves clang_toolchain_info on `platform` and
// returns its toolchain SBOM component (nil if the feature/platform is off).
// Callers invoke it for a module that compiled a C-family TU, mirroring the
// _SRC_CPP_TOOLCHAIN_INFO_PEER that those compiles induce.
func clangToolchainSbomComponent(ctx *GenCtx, platform *Platform) (*NodeRef, *VFS) {
	res := genModule(ctx, ModuleInstance{
		Path:     source(clangToolchainInfoRel),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: platform,
	})

	return res.SbomComponentRef, res.SbomComponentPath
}

// sbomActive reports whether _NEED_SBOM_INFO holds for this module instance:
// the feature is configured (ctx.sbomEnabled) and the platform is linux/x86_64
// (sbom.conf:4 — AUTOCHECK is never set in our runs).
func sbomActive(ctx *GenCtx, instance ModuleInstance) bool {
	return ctx.sbomEnabled && instance.Platform.OS == OSLinux && instance.Platform.ISA == ISAX8664
}

// sbomQualifies reports whether a module gets a _GEN_SBOM_COMPONENT — gated by a
// LICENSE() declaration (the _CONTRIB_MODULE_HOOKS trigger).
func sbomQualifies(d *ModuleData) bool {
	return d.hasLicense
}

// sbomComponentLang maps the module type to the uppercase MODULE_LANG token
// used in both the output suffix and the --lang argument: PY3 for python module
// types, CPP otherwise. (Driven by the module type, not the instance compile
// language, which is CPP even for the native half of a PY library.)
func sbomComponentLang(moduleName TOK) string {
	switch {
	case moduleName == tokPrebuiltProgram:
		return "AGNOSTIC"
	case pyModuleTypeUsesPython3(moduleName):
		return "PY3"
	default:
		return "CPP"
	}
}

// emitSbomComponent emits the per-module DX node (gen_sbom.py …
// <realPrjName>.<LANG>.component.sbom) and returns its ref and output path.
func emitSbomComponent(ctx *GenCtx, instance ModuleInstance, d *ModuleData, realPrjName string) (*NodeRef, *VFS) {
	na := ctx.na
	moddir := instance.Path.rel()
	lang := sbomComponentLang(d.moduleStmt.Name)
	modver := d.modver

	if modver == "" {
		modver = "unknown"
	}

	out := build(moddir + "/" + realPrjName + "." + lang + ".component.sbom")
	scriptVFS := source(sbomGenScriptRel)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// MODULE_TAG of the component — only the multimodule variants carry one (the
	// same value as their CC objects' module_tag): PY23_LIBRARY -> py3,
	// PY23_NATIVE_LIBRARY -> py3_native, PY3_PROGRAM -> py3_bin_lib (its
	// SRCS_GLOBAL-owning lib half). A plain PY3_LIBRARY / CPP carries none.
	var moduleTag STR

	switch d.moduleStmt.Name {
	case tokPy23Library:
		moduleTag = tagPy3
	case tokPy23NativeLibrary:
		moduleTag = tagPy3Native
	case tokPy3Program, tokPy3ProgramBin:
		moduleTag = tagPy3BinLib
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList([]STR{
			wrapccPython3STR,
			scriptVFS.str(),
			strOutput, out.str(),
			strType, strLibrary,
			strPath, internStr(moddir),
			strVer, internStr(modver),
			strLang, internStr(lang),
		}), Env: env}),
		Env:              env,
		Inputs:           na.inputList([]VFS{scriptVFS}),
		KV:               KV{P: pkDX, PC: pcYellow},
		Outputs:          na.vfsList(out),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: moddir, ModuleTag: moduleTag},
	}

	ref := ctx.emit.emit(node)

	return &ref, &out
}

// emitSbomToolchainComponent emits the TOOLCHAIN(Name) DX node (gen_sbom.py
// --type toolchain): <dir>/toolchain.component.sbom. Like the component variant
// it is a global output that propagates via the SBOM closure to consumers.
func emitSbomToolchainComponent(ctx *GenCtx, instance ModuleInstance, toolchainName, ver string) (*NodeRef, *VFS) {
	na := ctx.na
	moddir := instance.Path.rel()

	if ver == "" {
		ver = "unknown"
	}

	out := build(moddir + "/toolchain.component.sbom")
	scriptVFS := source(sbomGenScriptRel)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList([]STR{
			wrapccPython3STR,
			scriptVFS.str(),
			strOutput, out.str(),
			strType, strToolchain,
			strToolchainName, internStr(toolchainName),
			strVer, internStr(ver),
		}), Env: env}),
		Env:              env,
		Inputs:           na.inputList([]VFS{scriptVFS}),
		KV:               KV{P: pkDX, PC: pcYellow},
		Outputs:          na.vfsList(out),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: moddir},
	}

	ref := ctx.emit.emit(node)

	return &ref, &out
}
