package main

func isPythonDllModule(name TOK) bool {
	return name == tokPyAnyModule || name == tokPy2Module || name == tokPy3Module
}

func setupPythonDllModule(stmt *ModuleStmt, env Environment) {
	if !isPythonDllModule(stmt.Name) {
		return
	}

	env.setStringID(envMODULE_TYPE, kwDLL)
	env.setDefaultString(envPythonTypeForCython, "PY3")
	env.setDefaultString(envAntlrPython, "Python2")
}
