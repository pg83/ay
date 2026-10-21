package main

var taskletRegistrationKV = KV{P: pkTT, PC: pcYellow}

var taskletRegistrationPeers = internAnys([]string{
	"tasklet/v1/domain", "sandbox/bin", "sandbox/taskbox/worker",
})

var taskletRegistrationIncludes = internAnys([]string{
	"tasklet/v1/runtime/lib/cpp_wrapper.h",
	"tasklet/v1/runtime/lib/go_wrapper.h",
	"tasklet/v1/runtime/lib/py_wrapper.h",
	"tasklet/v1/runtime/lib/js_wrapper.h",
	"tasklet/v1/runtime/lib/registry.h",
})

func applyTaskletRegistration(stmt UnknownStmt, d *ModuleData, env Environment) {
	args := expandStmtTokens(stmt.Args, env)
	extended := stmt.Name == tokTaskletRegExt
	required := 3

	if extended {
		required = 4
	}

	if len(args) < required {
		throwFmt("%s expects at least %d positional arguments", stmt.Name.string(), required)
	}

	d.peerdirs = append(d.peerdirs, taskletRegistrationPeers...)

	if !extended && args[1].string() == "js" {
		d.peerdirs = append(d.peerdirs, strTaskletV1RuntimeJs.any())
	}

	output := internV(args[0].string(), ".task.cpp").any()
	command := []ANY{args[0], strL.any(), args[1], strI2.any(), args[2]}

	if extended {
		command = append(command, strW.any(), args[3])
	}

	command = append(command, output)
	command = append(command, args[required:]...)

	includes := append([]ANY(nil), args[required:]...)

	includes = append(includes, taskletRegistrationIncludes...)

	d.runPython = append(d.runPython, &RunPythonStmt{
		ScriptPath:     strBuildScriptsGenTaskletRegPy.any(),
		Args:           command,
		OUTNoAutoFiles: []ANY{output},
		OutputIncludes: includes,
		KV:             &taskletRegistrationKV,
		Line:           stmt.Line,
		DeclSeq:        d.nextDeclSeq(),
	})

	d.srcs = append(d.srcs, SrcMeta{Source: output, Prio: stmtPrioDefault, TaskletSeq: d.nextDeclSeq(), Global: true})
}
