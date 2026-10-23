package main

type PyProtoPlugin struct {
	Name           string
	ToolPath       string
	OutputSuffixes []string
	Deps           []ANY
}

func parsePyProtoPlugin(stmt UnknownStmt) PyProtoPlugin {
	positional, lists := goMacroLists(stmt.Args, "DEPS")
	suffixCount := 1

	if stmt.Name == tokPyProtoPlugin2 {
		suffixCount = 2
	}

	if len(positional) != suffixCount+2 {
		throwFmt("%s expects %d positional arguments, got %d", stmt.Name, suffixCount+2, len(positional))
	}

	return PyProtoPlugin{
		Name: positional[0].string(), ToolPath: positional[suffixCount+1].string(),
		OutputSuffixes: anyStrs(positional[1 : suffixCount+1]), Deps: lists["DEPS"],
	}
}

func pyProtoSuffixes(d *ModuleData) []string {
	suffixes := []string{"_pb2.py"}

	for _, plugin := range d.pyProtoPlugins {
		suffixes = append(suffixes, plugin.OutputSuffixes...)
	}

	return suffixes
}
