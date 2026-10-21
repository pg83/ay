package main

import (
	"path/filepath"
	"strings"
)

var ytRecordKV = KV{P: pkRC}

func applyYtRecord(modulePath string, v UnknownStmt, d *ModuleData, env Environment) {
	if len(v.Args) == 0 || len(v.Args) > 1 && v.Args[1].string() != "OUTPUT_INCLUDES" {
		throwFmt("GENERATE_YT_RECORD expects a YAML input and optional OUTPUT_INCLUDES")
	}

	input := v.Args[0]
	base := strings.TrimSuffix(input.string(), filepath.Ext(input.string()))
	cpp := copyFileOutputVFS(modulePath, base+".record.cpp").any()
	header := copyFileOutputVFS(modulePath, base+".record.h").any()
	args := []ANY{strInput.any(), input, strOutputRoot2.any(), argB.any(), strOutputCpp.any(), cpp}

	var includes []ANY

	if len(v.Args) > 1 {
		includes = v.Args[2:]
	}

	for _, include := range includes {
		args = append(args, strOutputInclude.any(), include)
	}

	d.runPrograms = append(d.runPrograms, &RunProgramStmt{
		KV:             &ytRecordKV,
		ToolPath:       strYtYtToolsRecordCodegenYtRecordCodegenBin.any(),
		Args:           args,
		INFiles:        []ANY{input},
		OUTFiles:       []ANY{cpp, header},
		OutputIncludes: append(append([]ANY(nil), includes...), strYtYtClientTableClientRecordCodegenDepsH.any()),
		Line:           v.Line,
		DeclSeq:        d.nextDeclSeq(),
	})

	key := envYtRecordPeerdir

	env.setDefaultString(key, "yt/yt/client")
	d.peerdirs = append(d.peerdirs, internAnys(strings.Fields(env.string(key)))...)
}
