package main

func setupUdfBaseModule(stmt *ModuleStmt, d *ModuleData, env Environment) {
	if stmt.Name != tokUdfBase {
		return
	}

	if parseDllModuleArgs(stmt.Args).deprecatedExports {
		throwFmt("gen: EXPORTS keyword in module header is deprecated and will be ignored. Use EXPORTS_SCRIPT macro instead.")
	}

	env.setStringID(envMODULE_TYPE, kwDLL)
	env.setStringID(envMODULE_TAG, kwDLL)
	env.setBool(envDynamicLink, true)
	applyAllocatorStmt(UnknownStmt{Args: []ANY{strFAKE.any()}}, d, env)
}
