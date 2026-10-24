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
	d.yqlPrebuiltTool = name == tokYqlPrebuiltTool
	d.yqlPrebuiltAliases = name == tokYqlPrebuiltProgram || name == tokYqlUdfContrib && d.unit.Type == tokDll

	setupYqlUdfModule(d, name, env)

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
		d.unit.Tag = strYqlUdfStaticCurrent
		d.unit.HashTag = d.unit.Tag
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

func finishYqlModule(ctx *GenCtx, instance ModuleInstance, env Environment, d *ModuleData, onWarn func(Warn)) {
	modulePath := instance.Path.relString()

	finishYqlUdfModule(modulePath, d, env)

	if !d.yqlPrebuiltAliases {
		return
	}

	d.yqlPrebuiltAliases = false
	d.yqlPrebuiltResource = selectYqlPrebuiltResource(modulePath, env, d)

	if d.yqlPrebuiltResource == "" {
		if d.yqlUdfShared {
			d.peerdirs = append(d.peerdirs, internAnys(yqlUdfImplicitPeers())...)
		}

		collectStmts(ctx, modulePath, instance.Kind, instance.Language, []Stmt{
			&SrcsStmt{Sources: d.yqlSourceSrcs}, &PeerdirStmt{Paths: d.yqlSourcePeers},
		}, env, d, onWarn, instance.Platform)

		return
	}

	d.flags.NoPlatform = true
	d.flags.NoLibc = true
	d.flags.NoRuntime = true
	d.flags.NoUtil = true

	collectStmts(ctx, modulePath, instance.Kind, instance.Language, []Stmt{
		&SetStmt{Name: "NEED_PLATFORM_PEERDIRS", NameEnv: envNeedPlatformPeerdirs, Value: "no"},
		&UnknownStmt{Name: tokDisable, Args: []ANY{strCommonLinkSettings.any()}},
		&UnknownStmt{Name: tokAllocator, Args: []ANY{strFAKE.any()}},
	}, env, d, onWarn, instance.Platform)

	prebuilt := path.Base(yqlPrebuiltOutput(instance, d).relString()) + ".prebuilt"

	d.fromSandboxes = append(d.fromSandboxes, &FromSandboxStmt{
		ResourceId: internStr(d.yqlPrebuiltResource).any(), File: true,
		ArchiveExtraction: ArchiveExtraction{
			Executable: true, Prefix: ".",
			Renames: []ANY{kwRESOURCE.any()}, OUTNoAutoFiles: []ANY{internStr(prebuilt).any()},
		},
	})
}

func selectYqlPrebuiltResource(modulePath string, env Environment, d *ModuleData) string {
	if env.bool(envOPENSOURCE) || env.bool(envBuildYqlFromSources) {
		return ""
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
		return ""
	}

	name := path.Base(modulePath)

	if d.yqlUdfShared {
		name = javaRealPrjName(modulePath, javaNameTwoDirs)
	}

	if len(d.moduleStmt.Args) > 0 {
		name = d.moduleStmt.Args[0].string()
	}

	resource := env.string(internEnv(strings.ToUpper(name) + "_RESOURCE_" + variant))

	if resource == "" || resource == "None" {
		return ""
	}

	return resource
}

func yqlPrebuiltOutput(instance ModuleInstance, d *ModuleData) VFS {
	name := programBinaryName(instance, d.moduleStmt)

	if d.yqlUdfShared {
		name = dllOutputName(instance, d.moduleStmt)
	}

	return lDOutputPath(instance, name)
}

func (e *EmitContext) genYqlPrebuiltTool() *ModuleEmitResult {
	if e.d.primaryOutput == "" || strings.Contains(e.d.primaryOutput, "${") {
		throwFmt("gen: %s: YQL_PREBUILT_TOOL requires a resolved PRIMARY_OUTPUT", e.instance.Path.relString())
	}

	globals := e.scriptModuleResources(nil, "build/platform/python/ymake_python3")

	e.emitDeclaredProducers(nil)

	src := e.resolveModuleSourceVFS(internStr(e.d.primaryOutput).any(), e.d.srcDirs)
	out := lDOutputPath(e.instance, programBinaryName(e.instance, e.d.moduleStmt))

	return e.genYqlPrebuiltCopy(src, out, globals)
}

func (e *EmitContext) genYqlPrebuiltCopy(src, out VFS, globals []ResourceDecl) *ModuleEmitResult {
	ctx, d, na := e.ctx, e.d, e.ctx.na

	var refs []NodeRef

	if info := e.codegen.use(src); info != nil {
		refs = na.refList(info.ProducerRef)
	}

	node := Node{
		Platform: e.instance.Platform,
		Cmds:     na.cmdList(e.pythonScriptCmd("fs_tools.py", "copy", src.string(), out.string())),
		Env:      envVarsVCS, KV: &resourcesKV, Outputs: na.vfsList(out),
		Inputs:  na.inputList(na.vfsList(src), ctx.scripts[copyFsToolsVFS.rel()]),
		DepRefs: refs, Resources: usesPython3,
	}

	ref := e.emitModuleNode(node, true)

	result := &ModuleEmitResult{
		ModuleStmtName: d.moduleStmt.Name, LDRef: ref, LDPath: ptr(out),
		ResourceGlobalClosure: globals, TestSuites: e.styleSuites(globals),
	}

	if !d.yqlUdfShared {
		result.ARRef, result.ARPath, result.isPROGRAM = ref, ptr(out), true
	}

	ctx.memoPut(e.instance, result)

	return result
}

func setupYqlUdfModule(d *ModuleData, name TOK, env Environment) {
	python := name == tokYqlPythonUdf || name == tokYqlPython3Udf

	if !python && !((name == tokYqlUdf || name == tokYqlUdfContrib) && d.unit.Type == tokDll) {
		return
	}

	d.yqlUdfShared = true
	d.yqlPythonUdf = python
	d.usePython3 = name == tokYqlPython3Udf
	d.cxxFlags = append(d.cxxFlags, argDbuildUdf.any())
	d.ldPlugins = append(d.ldPlugins, strYqlEssentialsPublicUdfServiceDropPolicyGlobalsPy.any())
	env.setDefaultString(envYqlUdfExport, "$(S)/yql/essentials/public/udf/udfs_exports.exports")

	if python {
		env.setDefaultString(envYqlPythonUdfExport, "$(S)/yql/essentials/udfs/common/python/python_udf/python_udfs_exports.exports")
	}
}

func finishYqlUdfModule(modulePath string, d *ModuleData, env Environment) {
	if !d.yqlUdfShared {
		return
	}

	d.yqlUdfProbe = !env.bool(envSANITIZER_TYPE) && env.bool(envUSE_ARCADIA_PYTHON) && !env.bool(envUdfNoProbe) && !env.bool(envMUSL) && !env.bool(envCppAnalysisMode)

	if d.yqlPythonUdf {
		peers := yqlUdfImplicitPeers()

		peers = append(peers, "build/platform/python/ldflags", "library/python/type_info")
		d.peerdirs = append(internAnys(peers), d.peerdirs...)
		peers = nil

		if env.bool(envUSE_ARCADIA_PYTHON) {
			if d.usePython2 {
				peers = append(peers, "library/python/runtime/main")

				if d.pythonSQLite3 {
					peers = append(peers, "contrib/tools/python/src/Modules/_sqlite")
				}
			} else {
				peers = append(peers, "library/python/runtime_py3/main")

				if d.pythonSQLite3 {
					peers = append(peers, "contrib/tools/python3/Modules/_sqlite")
				}
			}
		}

		d.peerdirs = append(d.peerdirs, internAnys(peers)...)
	}

	if d.exportsScript == nil && !env.bool(envOS_WINDOWS) {
		id := envYqlUdfExport

		if d.yqlPythonUdf {
			id = envYqlPythonUdfExport
		}

		if exports := env.string(id); exports != "" {
			if rooted, ok := moduleRootedVFS(modulePath, exports); ok {
				exports = rooted.relString()
			}

			d.exportsScript = ptr(internStr(exports).any())
		}
	}
}

func registerYqlPythonUdf(fs FS, modulePath string, stmt UnknownStmt, d *ModuleData, env Environment, onWarn func(Warn)) {
	name, resource, libra := "CustomPython", "", "no"

	for i := 0; i < len(stmt.Args); i += 2 {
		if i+1 == len(stmt.Args) {
			throwFmt("REGISTER_YQL_PYTHON_UDF: %s requires a value", stmt.Args[i].string())
		}

		value := stmt.Args[i+1].string()

		switch key := stmt.Args[i].string(); key {
		case "NAME":
			name = value
		case "RESOURCE_NAME":
			resource = value
		case "ADD_LIBRA_MODULES":
			libra = value
		default:
			throwFmt("REGISTER_YQL_PYTHON_UDF: unknown option %s", key)
		}
	}

	if name != "Python3" && name != "Python2" && name != "ArcPython2" && !strings.HasPrefix(name, "SystemPython") && !strings.Contains(name, "CustomPython") {
		throwFmt("Invalid python UDF module name")
	}

	if resource == "" {
		resource = name
	}

	addLibra := libra == "yes"

	if addLibra && env.string(envOPENSOURCE) != "" {
		throwFmt("Libra modules are not supported in opensource python UDFs")
	}

	applyUnknownStmt(fs, modulePath, UnknownStmt{Name: tokYqlAbiVersion, Args: internAnys([]string{"2", "45", "0"})}, d, env, onWarn)
	d.pluginPeerdirs = append(d.pluginPeerdirs, internAnys([]string{"yql/essentials/udfs/common/python/python_udf", "yql/essentials/public/udf"})...)

	includes := []string{"yql/essentials/udfs/common/python/python_udf/python_udf.h", "yql/essentials/public/udf/udf_registrator.h"}
	flag := "0"

	if addLibra {
		flag = "1"
		d.pluginPeerdirs = append(d.pluginPeerdirs, internAnys([]string{"quality/user_sessions/libra_arc/noyql", "yql/udfs/quality/libra/module"})...)
		includes = append(includes, "yql/udfs/quality/libra/module/module.h")
	}

	flavor := "System"

	if env.bool(envUSE_ARCADIA_PYTHON) {
		flavor = "Arcadia"

		dir := env.string(envYqlPythonDir)

		if dir == "" {
			dir = "yql/essentials/udfs/common/python"
		}

		peers := []string{"library/python/runtime", path.Join(dir, "main")}

		if env.bool(envPYTHON3) {
			peers = []string{"library/python/runtime_py3", "yql/essentials/udfs/common/python/main_py3"}
		}

		d.pluginPeerdirs = append(d.pluginPeerdirs, internAnys(peers)...)
	}

	output := name + ".yql_python_udf.cpp"

	d.runPython = append(d.runPython, &RunPythonStmt{
		ScriptPath: strBuildScriptsGenYqlPythonUdfPy.any(),
		Args:       internAnys([]string{flavor, name, resource, output, flag}),
		OUTFiles:   internAnys([]string{output}), OutputIncludes: internAnys(includes), Line: stmt.Line,
	})
}
