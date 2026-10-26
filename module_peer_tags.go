package main

func modulePeerTags(stmt *ModuleStmt, unit ModuleUnit, language Language) string {
	const cpp = "$_CPP_PROTO_COMPATIBLE_TAGS CPP_PROTO_FROM_SCHEMA"
	const abi = "$YQL_UDF_STATIC_PEERDIR_TAG $YQL_ABI_PEERDIR_TAG __EMPTY__ RESOURCE_LIB DLL_LIB"

	switch stmt.Name {
	case tokPackage, tokPackageFinal:
		return cpp + " CPP_FBS PY2 PY3 PY2_NATIVE PY3_NATIVE YQL_UDF_SHARED __EMPTY__ RESOURCE_LIB DOCSBOOK JAR_RUNNABLE PY3_BIN PY3TEST_PROGRAM DLL GO_PROGRAM TS PACKAGE_UNION"
	case tokCiGroup:
		return cpp + " PY3 PY3_NATIVE PY3_BIN PY3TEST_PROGRAM YQL_UDF_SHARED __EMPTY__ RESOURCE_LIB DOCSBOOK JAR_RUNNABLE FULL_JAR_COMPILATION DLL PACKAGE_FINAL"
	case tokGenLibrary:
		return "__EMPTY__ RESOURCE_LIB"
	case tokResourcesLibrary:
		return "RESOURCE_LIB"
	case tokDocsLibrary:
		return "DOCS_LIBRARY DOCS_PROTO"
	}

	if stmt.Name == tokProtoLibrary {
		tag := protoModuleTag(language, false)
		prefix := ""

		switch language {
		case LangCPP, LangProto, LangNone:
			tag = "$_CPP_PROTO_COMPATIBLE_TAGS"
		case LangPy:
			prefix = "PY3 "
		case LangPy2:
			prefix = "PY2 "
		case LangGo:
			prefix = "GO "
		case LangJava:
			prefix = "JAVA IJAR "
		}

		if stmt.Schema {
			tag += " " + protoModuleTag(language, true)
		}

		return prefix + tag
	}

	if isGoModuleType(unit.Type) {
		return "GO GO_PROTO GO_PROTO_FROM_SCHEMA GO_FBS GO_DIAG __EMPTY__ RESOURCE_LIB $YQL_ABI_PEERDIR_TAG"
	}

	if isPythonModuleType(unit.Type) {
		if unit.Tag == unitTagPy2 {
			return "PY2 PY2_NATIVE PY_PROTO PY_PROTO_FROM_SCHEMA PY2_FBS " + abi
		}

		return "PY3 PY3_BIN_LIB PY3TEST_LIBRARY PY3_NATIVE PY3_PROTO PY3_PROTO_FROM_SCHEMA PY3_FBS PY3_ROS PY3_MLAUNCH_LIB ALICE_NLG_LIB " + abi
	}

	if isUnionModule(unit.Type) {
		return cpp + " CPP_FBS PY3 PY2_NATIVE PY3_NATIVE YQL_UDF_SHARED __EMPTY__ RESOURCE_LIB DOCSBOOK JAR_RUNNABLE PY3_BIN DLL TS PACKAGE_UNION"
	}

	return cpp + " CPP_FBS CPP_ROS CPP_DIAG CPP_MLAUNCH CPP_PY3_MLAUNCH H_IDL PY3 PY3_NATIVE " + abi
}
