package main

import (
	"path"
	"strings"
)

func confYqlStableABI(fs FS) bool {
	const conf = "build/conf/project_specific/yql_udf.conf"

	return fs.isFile(srcRootRel, conf) && strings.Contains(string(concatChunks(fs.read(conf))), "macro YQL_STABLE_ABI_VERSION(")
}

func setupYqlModule(d *ModuleData, name TOK, env Environment) {
	if name != tokYqlLibrary && !isYqlUdfStaticModule(name) {
		return
	}

	if name == tokYqlLibrary {
		variant := "stable"

		if d.yqlCurrent {
			variant = "current"
		}

		tag := "YQL_ABI_" + strings.ToUpper(variant)

		d.unit.Type = tokLibrary
		d.unit.Tag = internStr(tag)
		d.unit.HashTag = d.unit.Tag
		d.unit.CCTag = d.unit.Tag
		d.unit.ARPrefix = "yql-abi-" + variant + "-"
		d.unit.ObjectSuffixStem = "abi_" + variant
		d.unit.GlobalARTag = internStr(tag + "_GLOBAL")
		d.cxxFlagsVar = append(d.cxxFlagsVar, internArg("-DUSE_"+strings.ToUpper(variant)+"_UDF_ABI_VERSION").any())
		env.setString(envMODULE_TAG, tag)
	} else if d.yqlCurrent {
		d.unit.ARPrefix = "yql-abi-current-"
		d.unit.ObjectSuffixStem = "udfs.abi_current"
		d.cxxFlagsVar = append(d.cxxFlagsVar, argDuseCurrentUdfAbiVersion.any())
	}

	if d.yqlCurrent {
		setYqlCurrentPeers(d, env)
	}
}

func setYqlCurrentPeers(d *ModuleData, env Environment) {
	d.yqlCurrent = true
	env.setString(envYqlAbiPeerdirTag, "YQL_ABI_CURRENT")
	env.setString(envYqlUdfStaticPeerdirTag, "YQL_UDF_STATIC_CURRENT")
}

func applyYqlABI(d *ModuleData, env Environment, current bool) {
	stated := "_YQL_ABI_STATED_STABLE"
	flag := "-DUSE_STABLE_UDF_ABI_VERSION"

	if current {
		stated = "_YQL_ABI_STATED_CURRENT"
		flag = "-DUSE_CURRENT_UDF_ABI_VERSION"
		setYqlCurrentPeers(d, env)
	}

	env.setBool(internEnv(stated), true)
	d.cxxFlags = append(d.cxxFlags, internArg(flag).any())

	if env.bool(envYqlAbiStatedStable) && env.bool(envYqlAbiStatedCurrent) {
		d.peerdirs = append(d.peerdirs, strYqlEssentialsPublicUdfAbiConflict.any())
	}
}

func finishYqlModule(modulePath string, env Environment, d *ModuleData) {
	if d.moduleStmt == nil || d.moduleStmt.Name != tokYqlPrebuiltProgram {
		return
	}

	if env.bool(envOPENSOURCE) || env.bool(envBuildYqlFromSources) {
		return
	}

	variant := "DEFAULT"

	switch sanitizer := env.string(envSANITIZER_TYPE); sanitizer {
	case "":
	case "address":
		variant = "ASAN"
	case "memory":
		variant = "MSAN"
	case "thread":
		variant = "TSAN"
	case "undefined":
		variant = "UBSAN"
	default:
		return
	}

	name := path.Base(modulePath)

	if len(d.moduleStmt.Args) > 0 {
		name = d.moduleStmt.Args[0].string()
	}

	resource := env.string(internEnv(strings.ToUpper(name) + "_RESOURCE_" + variant))

	if resource == "" || resource == "None" {
		return
	}

	d.yqlPrebuiltResource = resource
	d.srcs = nil
	d.peerdirs = nil
	d.flags.NoPlatform = true
	d.flags.NoLibc = true
	d.flags.NoRuntime = true
	d.flags.NoUtil = true
	d.hadAllocator = true
	d.allocatorName = strFAKE.any()
}

func (e *EmitContext) genYqlPrebuiltProgram() *ModuleEmitResult {
	ctx, d, na := e.ctx, e.d, e.ctx.na
	globals := e.scriptModuleResources(nil, "build/platform/python/ymake_python3")
	name := programBinaryName(e.instance, d.moduleStmt)
	prebuilt := name + ".prebuilt"
	src := build(e.instance.Path.relString(), "/", prebuilt)
	out := lDOutputPath(e.instance, name)

	e.emitFromSandbox(&FromSandboxStmt{
		ResourceId: internStr(d.yqlPrebuiltResource).any(), File: true,
		ArchiveExtraction: ArchiveExtraction{
			Executable: true, Prefix: ".",
			Renames: []ANY{kwRESOURCE.any()}, OUTNoAutoFiles: []ANY{internStr(prebuilt).any()},
		},
	})

	info := e.codegen.use(src)

	node := Node{
		Platform: e.instance.Platform,
		Cmds:     na.cmdList(e.pythonScriptCmd("fs_tools.py", "copy", src.string(), out.string())),
		Env:      envVarsVCS, KV: &resourcesKV, Outputs: na.vfsList(out),
		Inputs:  na.inputList(na.vfsList(src), ctx.scripts[copyFsToolsVFS.rel()]),
		DepRefs: na.refList(info.ProducerRef), Resources: usesPython3,
	}

	ref := e.emitNode(node)

	result := &ModuleEmitResult{
		ModuleStmtName: d.moduleStmt.Name, ARRef: ref, ARPath: ptr(out),
		LDRef: ref, LDPath: ptr(out), isPROGRAM: true, ResourceGlobalClosure: globals,
	}

	ctx.memoPut(e.instance, result)

	return result
}
