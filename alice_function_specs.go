package main

func aliceFunctionSpecStmts(args []ANY, line int) []Stmt {
	stmts := []Stmt{&UnknownStmt{
		Name: tokBundle,
		Args: internAnys([]string{"alice/functions/proto/proto_registry", "NAME", "functions_proto_registry.desc"}),
		Line: line,
	}}

	outputs := []string{
		"llm_function_specs.json",
		"llm_function_specs_all_versions.json",
		"llm_function_specs_namespaces.json",
		"llm_function_specs_en-GB.json",
		"llm_function_specs_en-GB_all_versions.json",
		"llm_function_specs_namespaces_en-GB.json",
	}

	runArgs := []string{
		"alice/functions/json_spec_generator/generator",
		"functions_proto_registry.desc", "${BINDIR}/llm_function_specs",
		"IN", "functions_proto_registry.desc",
	}

	for _, output := range outputs {
		runArgs = append(runArgs, "OUT_NOAUTO", "${BINDIR}/"+output)
	}

	stmts = append(stmts, parseRunProgram(internAnys(runArgs), line))

	if len(args) != 0 && args[0].string() == "DONT_ADD_TO_RESOURCE" {
		return stmts
	}

	for _, output := range outputs {
		stmts = append(stmts, &ResourceStmt{Args: internAnys([]string{"${BINDIR}/" + output, output}), Line: line})
	}

	return stmts
}
