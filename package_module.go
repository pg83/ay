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

func setupPackageModule(stmt *ModuleStmt, language Language, d *ModuleData) {
	if stmt.Name != tokPackage {
		return
	}

	d.multimodule = tokPackage
	d.moduleVariants = 0

	if language != LangPackage {
		d.selfPeers = append(d.selfPeers, LangPackage)
	}
}
