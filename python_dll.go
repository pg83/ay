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

func applyPythonModuleSelector(d *ModuleData, env Environment, python3 bool) {
	if python3 {
		d.pythonModule3 = true
		env.setString(envAntlrPython, "Python3")
		applyPython3Addincl(d, env)
	} else {
		d.pythonModule2 = true
		env.setString(envAntlrPython, "Python2")
		applyPython2Addincl(d, env, true)
	}
}

func finishPythonDllConfig(d *ModuleData, env Environment) {
	if env.string(envUSE_ARCADIA_PYTHON) == "yes" && env.string(envMsvc) == "yes" || env.string(envIS_CROSS_TOOLS) == "yes" {
		if d.pythonModule2 {
			d.peerdirs = append(d.peerdirs, strContribToolsPythonLib.any())
		}

		if d.pythonModule3 {
			peer, bound := env.lookup("PYTHON3_TOOL_PEERDIR")

			if !bound {
				peer = "contrib/tools/python3"

				if env.string(envUSE_PYTHON3_PREV) == "yes" {
					peer += "_prev"
				}
			}

			d.peerdirs = append(d.peerdirs, internStr(peer).any(), internV(peer, "/Lib").any())
		}
	}
}
