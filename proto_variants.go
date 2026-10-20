package main

var protoVariantTags = map[string]Language{
	"CPP_PROTO": LangCPP, "JAVA_PROTO": LangJava, "PY3_PROTO": LangPy,
	"GO_PROTO": LangGo, "DESC_PROTO": LangDescProto, "DOCS_PROTO": LangDocsProto,
	"TS_PROTO": LangTS, "TS_PREPARE_DEPS": LangTSPrepare,
}

const defaultProtoVariants = 1<<LangCPP | 1<<LangJava | 1<<LangPy | 1<<LangGo | 1<<LangDescProto

func applyProtoVariantTags(v UnknownStmt, d *ModuleData) {
	if v.Name == tokOnlyTags {
		d.protoVariants = 0

	}

	for _, arg := range v.Args {
		if lang, ok := protoVariantTags[arg.string()]; ok {
			if v.Name == tokExcludeTags {
				d.protoVariants &^= 1 << lang
			} else {
				d.protoVariants |= 1 << lang
			}
		}
	}
}

func (ctx *GenCtx) genProtoTargets(dir string, variants uint16, demand ModuleDemand) {
	for _, language := range []Language{LangJava, LangPy, LangGo, LangDescProto, LangDocsProto, LangTSPrepare, LangTS} {
		if variants&(1<<language) == 0 {
			continue
		}

		instance := ctx.rootInstance(dir, demand)

		instance.Language = language

		if language == LangJava {
			instance.Kind = KindJavaMain
		}

		ctx.roots[ModuleInstance{Path: instance.Path, Kind: instance.Kind, Language: language, Platform: instance.Platform}] = true

		result := genModule(ctx, instance)

		if result.java != nil {
			ctx.emit.result(ctx.javaBuilder(result.java).use(result.java.target))
		} else {
			if result.ARPath != nil {
				ctx.scannerForPlatform(instance.Platform).codegen.use(*result.ARPath)
			}

			ctx.emit.result(result.ARRef)

			if result.GlobalRef != nil {
				ctx.emit.result(*result.GlobalRef)
			}
		}

		ctx.emitModuleTests(result)
	}
}
