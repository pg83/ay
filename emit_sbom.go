package main

// SBOM (Software Bill of Materials) component generation. _GEN_SBOM_COMPONENT
// attaches to every module declaring a LICENSE(): under _NEED_SBOM_INFO (linux
// x86_64) and non-JAVA it produces a <REALPRJNAME>.<LANG>.component.sbom global
// output. That output propagates up the link closure; only RELEASE programs
// collect it, so a debug target links a licensed lib without pulling its
// component. Emitting one per licensed module and letting the closure +
// collection gate prune matches upstream's unconditional generation.

const sbomGenScriptRel = "build/internal/scripts/gen_sbom.py"

// sbomConfRel turns the SBOM feature on (SBOM_GENERATION_ALLOWED=yes); present
// only in the internal contour.
const sbomConfRel = "build/internal/conf/sbom.conf"

// clangToolchainInfoRel is the RESOURCES_LIBRARY that TOOLCHAIN(clang) tags; it
// is the _SRC_CPP_TOOLCHAIN_INFO_PEER PEERDIR every C-family compile carries.
const clangToolchainInfoRel = "build/internal/platform/clang_toolchain_info"

// clangToolchainSbomComponent resolves the toolchain compiler info on `platform`
// and returns its toolchain SBOM component (nil if the feature/platform is off).
// Called for a module that compiled a C-family TU, mirroring the
// _SRC_CPP_TOOLCHAIN_INFO_PEER those compiles induce.
func clangToolchainSbomComponent(ctx *GenCtx, platform *Platform) (*NodeRef, *VFS) {
	res := genModule(ctx, ModuleInstance{
		Path:     source(clangToolchainInfoRel),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: platform,
	})

	return res.SbomComponentRef, res.SbomComponentPath
}

// pythonToolchainInfoRel is the python platform RESOURCES_LIBRARY (TOOLCHAIN).
// The base of every module PEERDIRs it, so this toolchain SBOM component is a
// universal peer contribution; a module resolving no other SBOM peers (e.g. a
// bare-link PREBUILT_PROGRAM) still carries it.
const pythonToolchainInfoRel = "build/platform/python/ymake_python3"

// pythonToolchainSbomComponent resolves the python toolchain on `platform` and
// returns its toolchain SBOM component (nil if the feature/platform is off).
func pythonToolchainSbomComponent(ctx *GenCtx, platform *Platform) (*NodeRef, *VFS) {
	res := genModule(ctx, ModuleInstance{
		Path:     source(pythonToolchainInfoRel),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: platform,
	})

	return res.SbomComponentRef, res.SbomComponentPath
}

// sbomActive reports whether _NEED_SBOM_INFO holds: the feature is configured
// (ctx.sbomEnabled) and the platform is linux/x86_64.
func sbomActive(ctx *GenCtx, instance ModuleInstance) bool {
	return ctx.sbomEnabled && instance.Platform.OS == OSLinux && instance.Platform.ISA == ISAX8664
}

// sbomQualifies reports whether a module gets a _GEN_SBOM_COMPONENT — gated by a
// LICENSE() declaration (the _CONTRIB_MODULE_HOOKS trigger).
func sbomQualifies(d *ModuleData) bool {
	// A PROTO_LIBRARY with EXCLUDE_TAGS(CPP_PROTO) builds no CPP_PROTO submodule
	// — the only proto submodule keeping _NEED_SBOM_INFO=yes. The remaining
	// py-proto submodule does DISABLE(_NEED_SBOM_INFO), so the module emits no
	// component even with LICENSE(). Our model represents this py-only contour.
	if d.moduleStmt.Name == tokProtoLibrary && moduleExcludesTag(d, "CPP_PROTO") {
		return false
	}

	return d.hasLicense
}

// MODULE_LANG / SBOM component-language tokens (uppercase, as emitted in SBOM
// component names).
const (
	moduleLangTokenCpp      = "CPP"
	moduleLangTokenPy3      = "PY3"
	moduleLangTokenAgnostic = "AGNOSTIC"
)

// sbomComponentLang maps the module type to the uppercase MODULE_LANG token used
// in both the output suffix and the --lang argument: PY3 for python module types,
// CPP otherwise. Driven by the module type, not the instance compile language
// (which is CPP even for the native half of a PY library).
func sbomComponentLang(moduleName TOK) string {
	switch {
	case moduleName == tokPrebuiltProgram:
		return moduleLangTokenAgnostic
	case moduleName == tokPy23NativeLibrary:
		// The PY3 submodule of PY23_NATIVE_LIBRARY does SET(MODULE_LANG CPP),
		// so its SBOM component is <name>.CPP.component.sbom despite the py3 prefix.
		return moduleLangTokenCpp
	case pyModuleTypeUsesPython3(moduleName):
		return moduleLangTokenPy3
	default:
		return moduleLangTokenCpp
	}
}

// emitSbomComponent emits the per-module DX node producing
// <realPrjName>.<LANG>.component.sbom and returns its ref and output path.
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

	// MODULE_TAG of the component — only multimodule variants carry one (matching
	// their CC objects' module_tag): PY23_LIBRARY -> py3, PY23_NATIVE_LIBRARY ->
	// py3_native, PY3_PROGRAM -> py3_bin_lib, PROTO_LIBRARY -> cpp_proto (the
	// CPP_PROTO submodule that generates the .CPP component; sbomQualifies already
	// excluded the EXCLUDE_TAGS(CPP_PROTO) py-only case). Plain PY3_LIBRARY / CPP
	// carries none.
	var moduleTag STR

	switch d.moduleStmt.Name {
	case tokPy23Library:
		moduleTag = tagPy3
	case tokPy23NativeLibrary:
		moduleTag = tagPy3Native
	case tokPy3Program, tokPy3ProgramBin:
		moduleTag = tagPy3BinLib
	case tokProtoLibrary:
		moduleTag = tagCppProto
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
		Resources:        usesPython3,
	}

	ref := ctx.emit.emit(node)

	return &ref, &out
}

// emitSbomToolchainComponent emits the TOOLCHAIN(Name) DX node producing
// <dir>/toolchain.component.sbom. Like the component variant it is a global
// output propagating via the SBOM closure to consumers.
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
		Resources:        usesPython3,
	}

	ref := ctx.emit.emit(node)

	return &ref, &out
}
