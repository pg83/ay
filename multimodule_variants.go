package main

var multimoduleTags = map[TOK]map[string]Language{
	tokPackage: {"PACKAGE_FINAL": LangCPP, "PACKAGE_UNION": LangPackage},
	tokProtoLibrary: {
		"CPP_PROTO": LangCPP, "JAVA_PROTO": LangJava, "PY3_PROTO": LangPy, "GO_PROTO": LangGo,
		"DESC_PROTO": LangDescProto, "DOCS_PROTO": LangDocsProto, "TS_PROTO": LangTS, "TS_PREPARE_DEPS": LangTSPrepare,
	},
	tokFbsLibrary:      {"CPP_FBS": LangCPP, "PY3_FBS": LangPy, "GO_FBS": LangGo, "JAVA_FBS": LangJava},
	tokRosLibrary:      {"CPP_ROS": LangCPP, "PY3_CPP_ROS": LangRosBindings, "PY3_ROS": LangPy, "GO_ROS": LangGo},
	tokMlaunchLibrary:  {"CPP_MLAUNCH": LangCPP, "GO_MLAUNCH": LangGo, "UNION_MLAUNCH": LangPackage},
	tokMlaunchPipeline: {"PIPELINE": LangCPP, "GOLAUNCH": LangGo, "DEPLOY": LangPackage},
}

const defaultProtoVariants = 1<<LangCPP | 1<<LangJava | 1<<LangPy | 1<<LangGo | 1<<LangDescProto

func applyMultimoduleVariantTags(v UnknownStmt, d *ModuleData) {
	name := d.multimodule

	if name == 0 {
		name = tokProtoLibrary
	}

	tags := multimoduleTags[name]

	if v.Name == tokOnlyTags {
		d.moduleVariants = 0
	}

	for _, arg := range v.Args {
		if language, ok := tags[arg.string()]; ok {
			if resolveModuleUnit(name, KindLib, language).NoStartTarget {
				continue
			}

			if v.Name == tokExcludeTags {
				d.moduleVariants &^= 1 << language
			} else {
				d.moduleVariants |= 1 << language
			}
		}
	}
}

func multimoduleVariantTag(name TOK, language Language) string {
	for tag, lang := range multimoduleTags[name] {
		if lang == language {
			return tag
		}
	}

	return ""
}

func (ctx *GenCtx) genMultimoduleTargets(target DiscoveredModule, variants uint16, demand ModuleDemand) []*ModuleEmitResult {
	var results []*ModuleEmitResult
	var instances []ModuleInstance

	if target.Declaration != nil && target.Declaration.Name == tokYqlUdf {
		instance := ctx.rootInstanceForDeclaration(target.Path, demand, target.Declaration)

		instance.Kind = KindLib

		for _, current := range []bool{false, true} {
			instance.YqlCurrent = current
			instances = append(instances, instance)
		}
	}

	for _, language := range []Language{LangJava, LangPy, LangPy2, LangGo, LangDescProto, LangDocsProto, LangTSPrepare, LangTS, LangRosBindings, LangPackage} {
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

		result := genModule(ctx, instance)

		if result.java != nil {
			ctx.emit.result(ctx.javaBuilder(result.java).use(result.java.target))
		} else if result.LDPath != nil {
			ctx.scannerForPlatform(instance.Platform).codegen.use(*result.LDPath)
			ctx.emit.result(result.LDRef)
		} else if result.ARPath != nil {
			ctx.scannerForPlatform(instance.Platform).codegen.use(*result.ARPath)
			ctx.emit.result(result.ARRef)
		}

		if result.GlobalRef != nil {
			ctx.emit.result(*result.GlobalRef)
		}

		results = append(results, result)
	}

	return results
}
