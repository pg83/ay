package main

import (
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

var (
	strSdgSdcRosMonitoringsDiagUpdaterGenUtilsDiagsGoGen       = internStr(sdcDiagUpdaterDir + "/gen_utils/diags_go_gen")
	strSdgSdcRosMonitoringsDiagUpdaterGenUtilsDiagsSplitGen    = internStr(sdcDiagUpdaterDir + "/gen_utils/diags_split_gen")
	strSdgSdcRosMonitoringsDiagUpdaterUtilsDiagsSplitGenerator = internStr(sdcDiagUpdaterDir + "/utils/diags_split_generator")
)

const (
	sdcDiagUpdaterDir       = "sdg/sdc/ros/monitorings/diag_updater"
	sdcDiagDescriptionsRoot = sdcDiagUpdaterDir + "/etc/Descriptions"
)

func sdcDiagsModuleUnit(language Language) ModuleUnit {
	if language == LangGo {
		return resolveModuleUnitBare(tokGoLibrary, KindLib, language)
	}

	return resolveModuleUnitBare(tokLibrary, KindLib, language)
}

func setupSdcDiagsModule(stmt *ModuleStmt, language Language, env Environment, d *ModuleData) {
	if stmt.Name != tokSdcDiagsLibrary {
		return
	}

	d.multimodule = stmt.Name
	d.moduleVariants = 1<<LangCPP | 1<<LangGo

	if language == LangGo {
		env.setString(envMODULE_TAG, "GO_DIAG")

		d.peerdirs = append(d.peerdirs, internAnys([]string{
			env.string(envGOSTD) + "/fmt",
			"sdg/sdc/ros/configuration/golaunch/common_node_parameters/common_node_proto",
			"sdg/sdc/ros/configuration/golaunch/node",
			"sdg/sdc/ros/configuration/proto",
		})...)

		return
	}

	env.setString(envMODULE_TAG, "CPP_DIAG")

	include := sdgSdc

	d.addInclGlobal = append(d.addInclGlobal, include)
	d.addInclUserGlobal = append(d.addInclUserGlobal, include)
	d.addLocalIncl(prioAddIncl, include)
	d.peerdirs = append(d.peerdirs, internAnys([]string{"contrib/libs/protobuf", "sdg/sdc/myriapoda/yamakes/diagnostics/utils"})...)
}

func applySdcDiagsSplitGenerator(fs *Tree, modulePath string, stmt UnknownStmt, env Environment, d *ModuleData, onWarn func(Warn)) {
	files := anyStrs(stmt.Args)

	if len(files) == 0 {
		sdcDiagsPluginFailure(modulePath, stmt, onWarn)

		return
	}

	for i, f := range files {
		if resolved, ok := sourceInputVFS(fs, source(modulePath), f); ok {
			files[i] = resolved.relString()
		}
	}

	manifests := map[string]bool{}

	for _, f := range files {
		if !strings.HasPrefix(f, sdcDiagDescriptionsRoot) || f == sdcDiagDescriptionsRoot {
			sdcDiagsPluginFailure(modulePath, stmt, onWarn)

			return
		}

		if !strings.HasSuffix(f, ".yaml") {
			return
		}
	}

	if mode := env.string(envDiagGenMode); mode != "" && mode != "go" && mode != "cpp" {
		sdcDiagsPluginFailure(modulePath, stmt, onWarn)

		return
	}

	for _, f := range files {
		for dir := path.Dir(f); dir != sdcDiagDescriptionsRoot && strings.HasPrefix(dir, sdcDiagDescriptionsRoot); dir = path.Dir(dir) {
			manifests[dir+"/_manifest_.yaml"] = true
		}
	}

	deps := slices.Clone(files)

	for _, m := range slices.Sorted(maps.Keys(manifests)) {
		deps = append(deps, m)
	}

	rp := &RunProgramStmt{INFiles: internAnys(deps), Line: stmt.Line}

	if isGoModuleType(d.unit.Type) {
		var outs []string

		for _, f := range files {
			if name := path.Base(f); !strings.HasSuffix(name, "_manifest_.yaml") {
				outs = append(outs, strings.TrimSuffix(name, ".yaml")+".go")
			}
		}

		rp.ToolPath = strSdgSdcRosMonitoringsDiagUpdaterGenUtilsDiagsGoGen.any()
		rp.Args = internAnys(append(append([]string{"--description_files"}, deps...), "--out_dir", "${BINDIR}", "--package", path.Base(modulePath)))
		rp.OUTFiles = internAnys(outs)

		appendRunProgram(rp, env, d)

		return
	}

	resultDir := "${ARCADIA_BUILD_ROOT}/" + sdcDiagUpdaterDir + "/generated"

	var includes, srcs []string

	for _, f := range files {
		rel := strings.TrimSuffix(strings.TrimPrefix(f, sdcDiagDescriptionsRoot+"/"), ".yaml")

		includes = append(includes, resultDir+"/"+rel+".h")
		srcs = append(srcs, resultDir+"/"+rel+".cpp")
	}

	rp.ToolPath = strSdgSdcRosMonitoringsDiagUpdaterGenUtilsDiagsSplitGen.any()
	rp.Args = internAnys(append(append([]string{"--description_files"}, deps...), "--out_dir", resultDir))
	rp.OUTNoAutoFiles = internAnys(concat(includes, srcs))

	rp.OutputIncludes = internAnys([]string{
		"sdg/sdc/myriapoda/diagnostics/diags_utils.h",
		"contrib/libs/protobuf/src/google/protobuf/io/coded_stream.h",
		"contrib/libs/protobuf/src/google/protobuf/io/zero_copy_stream_impl_lite.h",
		"contrib/libs/protobuf/src/google/protobuf/wire_format_lite.h",
	})

	appendRunProgram(rp, env, d)

	for _, src := range expandStmtTokens(internAnys(concat(includes, srcs)), env) {
		d.srcs = append(d.srcs, SrcMeta{Source: src, Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), Global: true})
	}

	d.peerdirs = append(d.peerdirs, strContribLibsProtobuf.any())
}

func sdcDiagV3Output(rel string) string {
	base := "${ARCADIA_BUILD_ROOT}/" + sdcDiagUpdaterDir + "/gen_diags/diagnostics/v3/"

	if strings.HasSuffix(rel, ".yaml") {
		return base + strings.TrimSuffix(rel, ".yaml") + ".h"
	}

	return base + rel + "/_group.h"
}

func sdcDiagsPluginFailure(modulePath string, stmt UnknownStmt, onWarn func(Warn)) {
	name := stmt.Name.string()
	args := make([]string, len(stmt.Args))

	for i, arg := range stmt.Args {
		args[i] = pluginArgLiteral(arg)
	}

	onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: name + "(" + strings.Join(args, " ") + ") was not processed properly: devtools/ymake/plugins/ymake_module.cpp:189: Invalid call: report_configure_error. Skip " + name})
}

func applySdcDiagsSplitGeneratorV3(fs *Tree, modulePath string, stmt UnknownStmt, env Environment, d *ModuleData, onWarn func(Warn)) {
	if len(stmt.Args) != 1 {
		sdcDiagsPluginFailure(modulePath, stmt, onWarn)

		return
	}

	file := stmt.Args[0].string()

	if resolved, ok := sourceInputVFS(fs, source(modulePath), file); ok {
		file = resolved.relString()
	} else if fs.isDir(dirKey(modulePath), file) {
		file = sourceJoinClean(modulePath, file).relString()
	} else if fs.isDir(srcRootRel, file) {
		file = sourceClean(file).relString()
	}

	if !strings.HasPrefix(file, sdcDiagDescriptionsRoot) || file == sdcDiagDescriptionsRoot {
		sdcDiagsPluginFailure(modulePath, stmt, onWarn)

		return
	}

	group := !strings.HasSuffix(file, ".yaml")
	parent := path.Dir(file)
	hasParent := parent != sdcDiagDescriptionsRoot
	rel := throw2(filepath.Rel(sdcDiagDescriptionsRoot, file))
	output := sdcDiagV3Output(rel)
	input := file

	if group {
		input += "/_manifest_.yaml"
	}

	var groupArgs []string

	parentOutput := ""

	if hasParent {
		parentOutput = sdcDiagV3Output(path.Dir(rel))
		groupArgs = []string{"--group-include-file", strings.TrimPrefix(parentOutput, "${ARCADIA_BUILD_ROOT}/"+sdcDiagUpdaterDir+"/gen_diags/")}
	}

	var args []string

	if group {
		args = []string{"--group-manifest", input}
	} else {
		args = append(args, groupArgs...)
		args = append(args, "--diag-file", input)
	}

	namespace := strings.TrimSuffix(rel, path.Ext(rel))

	args = append(args, "--cpp", output, "--cpp-namespace", "yandex::sdc::diagnostics::v3::"+strings.ReplaceAll(namespace, "/", "::"))

	if group {
		args = append(args, groupArgs...)
	}

	includes := []string{"${ARCADIA_BUILD_ROOT}/sdg/sdc/ros/misc/protocol/DiagSeverity.h"}

	if hasParent {
		includes = []string{parentOutput, sdcDiagUpdaterDir + "/include/diagnostics/group.h"}
	}

	includes = append(includes, sdcDiagUpdaterDir+"/include/diagnostics/diagnostic.h")

	appendRunProgram(&RunProgramStmt{
		ToolPath: strSdgSdcRosMonitoringsDiagUpdaterUtilsDiagsSplitGenerator.any(),
		Args:     internAnys(args), INFiles: internAnys([]string{input}),
		OUTFiles: internAnys([]string{output}), OutputIncludes: internAnys(includes), Line: stmt.Line,
	}, env, d)
}
