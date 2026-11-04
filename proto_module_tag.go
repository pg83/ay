package main

func protoModuleTag(language Language, schema bool) string {
	owner := tokProtoLibrary.str()

	if schema {
		owner = strProtoSchema
	}

	return multimoduleSub(owner, KindLib, language, false).string()
}
