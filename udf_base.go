package main

func setupUdfBaseModule(stmt *ModuleStmt, d *ModuleData, env Environment) {
	if stmt.Name != tokUdfBase {
		return
	}

	env.setStringID(envMODULE_TYPE, kwDLL)
	env.setStringID(envMODULE_TAG, kwDLL)
	env.setBool(envDynamicLink, true)
}
