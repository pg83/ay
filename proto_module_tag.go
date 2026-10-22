package main

func protoModuleTag(language Language, schema bool) string {
	if language == LangProtoDescriptions {
		return "PROTO_DESCRIPTIONS_"
	}

	tag := multimoduleVariantTag(tokProtoLibrary, language)

	if language == LangPy2 {
		tag = "PY_PROTO"
	}

	if schema && language != LangTSPrepare {
		tag += "_FROM_SCHEMA"
	}

	return tag
}
