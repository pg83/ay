package main

var multimoduleTags = map[TOK]map[string]Language{
	tokPy23Library:       {"PY2": LangPy2, "PY3": LangCPP},
	tokPy23NativeLibrary: {"PY2": LangPy2, "PY3": LangCPP},
	tokPackage:           {"PACKAGE_FINAL": LangCPP, "PACKAGE_UNION": LangPackage},
	tokProtoLibrary: {
		"CPP_PROTO": LangCPP, "JAVA_PROTO": LangJava, "PY3_PROTO": LangPy, "GO_PROTO": LangGo,
		"DESC_PROTO": LangDescProto, "DOCS_PROTO": LangDocsProto, "TS_PROTO": LangTS, "TS_PREPARE_DEPS": LangTSPrepare,
	},
	tokFbsLibrary:      {"CPP_FBS": LangCPP, "GO_FBS": LangGo},
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

	for _, language := range []Language{LangJava, LangPy, LangPy2, LangGo, LangDescProto, LangDocsProto, LangTSPrepare, LangTS, LangRosBindings, LangPackage} {
		if variants&(1<<language) == 0 {
			continue
		}

		instance := ctx.rootInstanceForDeclaration(target.Path, demand, target.Declaration)

		instance.Language = language

		if language == LangJava {
			instance.Kind = KindJavaMain
		}

		ctx.roots[ModuleInstance{Path: instance.Path, Kind: instance.Kind, Language: language, Platform: instance.Platform}] = true

		result := genModule(ctx, instance)

		if result.java != nil {
			ctx.emit.result(ctx.javaBuilder(result.java).use(result.java.target))
		} else if result.LDRef != 0 {
			if result.LDPath != nil {
				ctx.scannerForPlatform(instance.Platform).codegen.use(*result.LDPath)
			}

			ctx.emit.result(result.LDRef)
		} else {
			if result.ARPath != nil {
				ctx.scannerForPlatform(instance.Platform).codegen.use(*result.ARPath)
			}

			ctx.emit.result(result.ARRef)

			if result.GlobalRef != nil {
				ctx.emit.result(*result.GlobalRef)
			}
		}

		results = append(results, result)
	}

	return results
}
