package main

func packageModuleUnit(language Language) ModuleUnit {
	name, tag := tokPackageFinal, "PACKAGE_FINAL"

	if language == LangPackage {
		name, tag = tokUnion, "PACKAGE_UNION"
	}

	unit := resolveModuleUnitBare(name, KindLib, language)

	unit.Tag = internStr(tag)
	unit.NoStartTarget = language == LangPackage

	return unit
}

func setupPackageModule(stmt *ModuleStmt, d *ModuleData) {
	if stmt.Name != tokPackage {
		return
	}

	d.multimodule = tokPackage
	d.moduleVariants = 0
}
