package main

var (
	shapeLibCPP     = variantShape{kind: KindLib, language: LangCPP}
	shapeBinCPP     = variantShape{kind: KindBin, language: LangCPP}
	shapeLibPy      = variantShape{kind: KindLib, language: LangPy}
	shapeLibPy2     = variantShape{kind: KindLib, language: LangPy2}
	shapeLibGo      = variantShape{kind: KindLib, language: LangGo}
	shapeJava       = variantShape{kind: KindJavaMain, language: LangJava}
	shapeLibPackage = variantShape{kind: KindLib, language: LangPackage}
)

var multimoduleShapes = map[STR]map[STR]variantShape{
	tokProtoLibrary.str(): shapeTable(map[string]variantShape{
		"CPP_PROTO": shapeLibCPP, "JAVA_PROTO": shapeJava, "PY3_PROTO": shapeLibPy, "PY_PROTO": shapeLibPy2, "GO_PROTO": shapeLibGo,
		"DESC_PROTO": {kind: KindLib, language: LangDescProto}, "DOCS_PROTO": {kind: KindLib, language: LangDocsProto},
		"TS_PROTO": {kind: KindLib, language: LangTS}, "TS_PREPARE_DEPS": {kind: KindLib, language: LangTSPrepare},
	}),
	strProtoSchema: shapeTable(map[string]variantShape{
		"CPP_PROTO_FROM_SCHEMA": shapeLibCPP, "JAVA_PROTO_FROM_SCHEMA": shapeJava, "PY3_PROTO_FROM_SCHEMA": shapeLibPy,
		"PY_PROTO_FROM_SCHEMA": shapeLibPy2, "GO_PROTO_FROM_SCHEMA": shapeLibGo, "DESC_PROTO_FROM_SCHEMA": {kind: KindLib, language: LangDescProto},
		"TS_PROTO_FROM_SCHEMA": {kind: KindLib, language: LangTS}, "TS_PREPARE_DEPS": {kind: KindLib, language: LangTSPrepare},
		"PROTO_DESCRIPTIONS_": {kind: KindLib, language: LangProtoDescriptions},
	}),
	tokFbsLibrary.str(): shapeTable(map[string]variantShape{"CPP_FBS": shapeLibCPP, "PY3_FBS": shapeLibPy, "GO_FBS": shapeLibGo, "JAVA_FBS": shapeJava}),
	tokRosLibrary.str(): shapeTable(map[string]variantShape{
		"CPP_ROS": shapeLibCPP, "PY3_CPP_ROS": {kind: KindLib, language: LangRosBindings}, "PY3_ROS": shapeLibPy, "GO_ROS": shapeLibGo,
	}),
	tokMlaunchLibrary.str():    shapeTable(map[string]variantShape{"CPP_MLAUNCH": shapeLibCPP, "GO_MLAUNCH": shapeLibGo, "UNION_MLAUNCH": shapeLibPackage}),
	tokMlaunchComponent.str():  shapeTable(map[string]variantShape{"CPP_MLAUNCH": shapeLibCPP, "GO_MLAUNCH": shapeLibGo, "UNION_MLAUNCH": shapeLibPackage}),
	tokMlaunchPipeline.str():   shapeTable(map[string]variantShape{"PIPELINE": shapeLibCPP, "GOLAUNCH": shapeLibGo, "DEPLOY": shapeLibPackage}),
	tokSdcDiagsLibrary.str():   shapeTable(map[string]variantShape{"CPP_DIAG": shapeLibCPP, "GO_DIAG": shapeLibGo}),
	tokSdcUnion.str():          shapeTable(map[string]variantShape{"SDC_INSTALL_ALL": shapeLibCPP, "SDC_INSTALL_SHARE": shapeLibPackage}),
	tokPackage.str():           shapeTable(map[string]variantShape{"PACKAGE_FINAL": shapeLibCPP, "PACKAGE_UNION": shapeLibPackage}),
	tokCrate.str():             shapeTable(map[string]variantShape{"RUST": shapeLibCPP, "RUST_HOST": {kind: KindLib, language: LangRustHost}}),
	tokPy23Library.str():       shapeTable(map[string]variantShape{"PY2": shapeLibPy2, "PY3": shapeLibCPP}),
	tokPy23NativeLibrary.str(): shapeTable(map[string]variantShape{"PY2": shapeLibPy2, "PY3": shapeLibCPP}),
	tokPy3Program.str():        shapeTable(map[string]variantShape{"PY3_BIN": shapeBinCPP, "PY3_BIN_LIB": shapeLibCPP}),
	tokSandboxTask.str():       shapeTable(map[string]variantShape{"PY2": shapeLibPy2, "SB_TASK_BIN": shapeBinCPP}),
	tokSandboxPy23Task.str():   shapeTable(map[string]variantShape{"PY2": shapeLibPy2, "PY3": shapeLibCPP, "SB_TASK_BIN": shapeBinCPP}),
	tokSandboxPy233Task.str():  shapeTable(map[string]variantShape{"PY2": shapeLibPy2, "PY3": shapeLibCPP, "SB_TASK_BIN": shapeBinCPP}),
	tokSandboxPy3Task.str():    shapeTable(map[string]variantShape{"PY3": shapeLibCPP, "SB_TASK_BIN": shapeBinCPP}),
	tokYqlUdf.str():            yqlUdfShapes(),
	tokYqlUdfYdb.str():         yqlUdfShapes(),
	tokYqlUdfContrib.str():     yqlUdfShapes(),
	tokYqlLibrary.str(): shapeTable(map[string]variantShape{
		"YQL_LIBRARY_STABLE": shapeLibCPP, "YQL_LIBRARY_CURRENT": {kind: KindLib, language: LangCPP, yqlCurrent: true},
	}),
	tokDllJava.str(): shapeTable(map[string]variantShape{"JNI_DLL": shapeLibCPP, "JAR_COMPILATION": shapeJava}),
}

const defaultProtoVariants = 1<<LangCPP | 1<<LangJava | 1<<LangPy | 1<<LangGo | 1<<LangDescProto

func multimoduleOwner(stmt *ModuleStmt) STR {
	if stmt == nil {
		return 0
	}

	if stmt.Schema && stmt.Name == tokProtoLibrary {
		return strProtoSchema
	}

	return stmt.Name.str()
}

func multimoduleSub(owner STR, kind ModuleKind, language Language, yqlCurrent bool) STR {
	var sub STR

	for name, shape := range multimoduleShapes[owner] {
		if shape.language == language && shape.yqlCurrent == yqlCurrent && (sub == 0 || (shape.kind == KindLib) == (kind == KindLib)) {
			sub = name
		}
	}

	return sub
}

func applyMultimoduleVariantTags(v UnknownStmt, d *ModuleData) {
	name := d.multimodule

	if name == 0 {
		name = tokProtoLibrary
	}

	shapes := multimoduleShapes[name.str()]

	if v.Name == tokOnlyTags {
		d.moduleVariants = 0
	}

	for _, arg := range v.Args {
		if shape, ok := shapes[arg.str()]; ok {
			if resolveModuleUnit(name, KindLib, shape.language).NoStartTarget {
				continue
			}

			if v.Name == tokExcludeTags {
				d.moduleVariants &^= 1 << shape.language
			} else {
				d.moduleVariants |= 1 << shape.language
			}
		}
	}
}

func (ctx *GenCtx) genMultimoduleTargets(target DiscoveredModule, variants uint16, demand ModuleDemand) []*ModuleEmitResult {
	var results []*ModuleEmitResult
	var instances []ModuleInstance

	if target.Declaration != nil && isSandboxPythonTask(target.Declaration.Name) {
		instance := ctx.rootInstanceForDeclaration(target.Path, demand, target.Declaration)

		for _, sub := range sandboxPythonTaskInstances(instance, target.Declaration.Name) {
			if (sub.Kind != instance.Kind || sub.Language != instance.Language) && variants&sandboxPythonTaskVariantBit(sub) != 0 {
				instances = append(instances, sub)
			}
		}

		variants = 0
	}

	if target.Declaration != nil && target.Declaration.Name == tokYqlUdf {
		instance := ctx.rootInstanceForDeclaration(target.Path, demand, target.Declaration)

		instance.Kind = KindLib

		for _, current := range []bool{false, true} {
			instance.YqlCurrent = current
			instances = append(instances, instance)
		}
	}

	for _, language := range []Language{LangJava, LangPy, LangPy2, LangGo, LangDescProto, LangDocsProto, LangTS, LangRosBindings, LangPackage} {
		if variants&(1<<language) == 0 {
			continue
		}

		instance := ctx.rootInstanceForDeclaration(target.Path, demand, target.Declaration)

		instance.Language = language

		if language == LangJava {
			instance.Kind = KindJavaMain
		}

		instances = append(instances, instance)
	}

	for _, instance := range instances {
		ctx.roots[ModuleInstance{Path: instance.Path, Kind: instance.Kind, Language: instance.Language, Platform: instance.Platform, YqlCurrent: instance.YqlCurrent}] = true
		ctx.rootPaths[instance.Path] = true

		result := genModule(ctx, instance)

		if result.java != nil {
			ref := ctx.javaBuilder(result.java).use(result.java.target)

			ctx.emit.result(ref)
		} else {
			for _, ref := range ctx.moduleResultRefs(instance, result) {
				ctx.emit.result(ref)
			}
		}

		ctx.emitGlobalResults(result)

		results = append(results, result)
	}

	return results
}

type variantShape struct {
	kind       ModuleKind
	language   Language
	yqlCurrent bool
}

func yqlUdfShapes() map[STR]variantShape {
	return shapeTable(map[string]variantShape{
		"YQL_UDF_SHARED": shapeBinCPP, "YQL_UDF_STATIC": shapeLibCPP, "YQL_UDF_STATIC_CURRENT": {kind: KindLib, language: LangCPP, yqlCurrent: true},
	})
}

func shapeTable(shapes map[string]variantShape) map[STR]variantShape {
	out := make(map[STR]variantShape, len(shapes))

	for sub, shape := range shapes {
		out[internStr(sub)] = shape
	}

	return out
}
