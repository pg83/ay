package main

func isPythonDllModule(name TOK) bool {
	return name == tokPyAnyModule || name == tokPy2Module || name == tokPy3Module
}

func setupPythonDllModule(stmt *ModuleStmt, env Environment) {
	if !isPythonDllModule(stmt.Name) {
		return
	}

	args := parseDllModuleArgs(stmt.Args)

	if args.deprecatedExports {
		throwFmt("gen: EXPORTS keyword in module header is deprecated and will be ignored. Use EXPORTS_SCRIPT macro instead.")
	}

	env.setStringID(envMODULE_TYPE, kwDLL)
	env.setDefaultString(envPythonTypeForCython, "PY3")
	env.setDefaultString(envAntlrPython, "Python2")
}
