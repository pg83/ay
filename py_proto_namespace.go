package main

import "strings"

func applyInlinePySrcs(fs FS, modulePath string, args []ANY, d *ModuleData, env Environment, onWarn func(Warn)) {
	before := len(d.pySrcGroups)

	applyUnknownStmt(fs, modulePath, UnknownStmt{Name: tokPySrcs, Args: args}, d, env, onWarn)

	for i := before; i < len(d.pySrcGroups); i++ {
		d.pySrcGroups[i].Prio = stmtPrioDefault
	}
}

func collectProtoToNamespace(fs FS, modulePath string, stmt UnknownStmt, d *ModuleData, env Environment, onWarn func(Warn)) {
	if len(stmt.Args) == 0 {
		throwFmt("Not allowed empty PROTO_TO_NAMESPACE")
	}

	generateInit := false
	prefix := ""

	var modules []string

	for i, arg := range stmt.Args {
		value := arg.string()

		if value == "IMPORT_ALL_IN_INIT" {
			generateInit = true

			continue
		}

		if strings.HasPrefix(value, "IN_") {
			if value != "IN_MSG" && value != "IN_PROTO" {
				throwFmt("Not allowed prefix %s in PROTO_TO_NAMESPACE", value)
			}

			if prefix != "" {
				throwFmt("Not allowed multiple prefixes in PROTO_TO_NAMESPACE")
			}

			prefix = strings.ToLower(strings.TrimPrefix(value, "IN_")) + "/"

			if i+1 == len(stmt.Args) {
				throwFmt("Not allowed empty value after %s in PROTO_TO_NAMESPACE", value)
			}

			continue
		}

		modules = append(modules, value)
	}

	var pys []ANY

	addWrapper := func(file string, imports []string) {
		out := internStr(prefix + file).any()
		args := make([]ANY, len(imports))

		for i, module := range imports {
			args[i] = internV("--from-import=", module).any()
		}

		d.runPrograms = append(d.runPrograms, &RunProgramStmt{
			ToolPath: strSdgTier0ToolsGenerateProxyImportFile.any(), Args: args,
			StdoutFile: &out, StdoutNoAuto: true, DeclSeq: d.nextDeclSeq(),
		})

		pys = append(pys, out)
	}

	for _, module := range modules {
		addWrapper(module+".py", []string{module})
	}

	if generateInit {
		addWrapper("__init__.py", modules)
	}

	if len(pys) > 0 {
		applyInlinePySrcs(fs, modulePath, pys, d, env, onWarn)
	}
}
