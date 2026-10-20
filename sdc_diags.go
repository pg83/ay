package main

import (
	"maps"
	"path"
	"slices"
	"strings"
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

func applySdcDiagsSplitGenerator(modulePath string, stmt UnknownStmt, env Environment, d *ModuleData) {
	files := anyStrs(stmt.Args)

	if len(files) == 0 {
		throwFmt("SDC_DIAGS_SPLIT_GENERATOR_V4: diagnostics file path is required")
	}

	manifests := map[string]bool{}

	for _, f := range files {
		if !strings.HasPrefix(f, sdcDiagDescriptionsRoot+"/") || !strings.HasSuffix(f, ".yaml") {
			throwFmt("SDC_DIAGS_SPLIT_GENERATOR_V4: %s must be a .yaml inside %s", f, sdcDiagDescriptionsRoot)
		}

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
			if name := path.Base(f); name != "_manifest_.yaml" {
				outs = append(outs, strings.TrimSuffix(name, ".yaml")+".go")
			}
		}

		rp.ToolPath = internStr(sdcDiagUpdaterDir + "/gen_utils/diags_go_gen").any()
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

	rp.ToolPath = internStr(sdcDiagUpdaterDir + "/gen_utils/diags_split_gen").any()
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
		d.srcs = append(d.srcs, SrcMeta{Source: src, Prio: stmtPrioDefault, Global: true})
	}

	d.peerdirs = append(d.peerdirs, strContribLibsProtobuf.any())
}
