package main

import "strings"

var parserExtMatcher = NewExtMatcher([]ExtEntry[IncludeDirectiveParser]{
	{".y", CIncludeDirectiveParser{}},
	{".l", CIncludeDirectiveParser{}},
	{".m", CIncludeDirectiveParser{}},
	{".C", CIncludeDirectiveParser{}},
	{".S", CIncludeDirectiveParser{}},
	{".c", CIncludeDirectiveParser{}},
	{".s", CIncludeDirectiveParser{}},
	{".H", CIncludeDirectiveParser{}},
	{".h", CIncludeDirectiveParser{}},
	{".go", CIncludeDirectiveParser{}},
	{".g4", EmptyIncludeDirectiveParser{}},
	{".mm", CIncludeDirectiveParser{}},
	{".xh", CIncludeDirectiveParser{}},
	{".hh", CIncludeDirectiveParser{}},
	{".rl", RagelIncludeDirectiveParser{}},
	{".cc", CIncludeDirectiveParser{}},
	{".m4", EmptyIncludeDirectiveParser{}},
	{".cu", CIncludeDirectiveParser{}},
	{".ev", ProtoIncludeDirectiveParser{}},
	{".rh", RagelIncludeDirectiveParser{}},
	{".fbs", FlatbuffersIncludeDirectiveParser{}},
	{".asi", YasmIncludeDirectiveParser{}},
	{".pyx", CythonIncludeDirectiveParser{}},
	{".stg", EmptyIncludeDirectiveParser{}},
	{".swg", SwigIncludeDirectiveParser{}},
	{".ypp", CIncludeDirectiveParser{}},
	{".rli", RagelIncludeDirectiveParser{}},
	{".rl5", RagelIncludeDirectiveParser{}},
	{".pxi", CythonIncludeDirectiveParser{}},
	{".rl6", RagelIncludeDirectiveParser{}},
	{".inl", CIncludeDirectiveParser{}},
	{".gzt", ProtoIncludeDirectiveParser{}},
	{".asm", YasmIncludeDirectiveParser{}},
	{".lex", CIncludeDirectiveParser{}},
	{".pxd", CythonIncludeDirectiveParser{}},
	{".cpp", CIncludeDirectiveParser{}},
	{".asp", CIncludeDirectiveParser{}},
	{".cxx", CIncludeDirectiveParser{}},
	{".hpp", CIncludeDirectiveParser{}},
	{".ipp", CIncludeDirectiveParser{}},
	{".hxx", CIncludeDirectiveParser{}},
	{".ixx", CIncludeDirectiveParser{}},
	{".lpp", CIncludeDirectiveParser{}},
	{".cuh", CIncludeDirectiveParser{}},
	{".sfdl", CIncludeDirectiveParser{}},
	{".geom", CIncludeDirectiveParser{}},
	{".tesc", CIncludeDirectiveParser{}},
	{".comp", CIncludeDirectiveParser{}},
	{".tese", CIncludeDirectiveParser{}},
	{".frag", CIncludeDirectiveParser{}},
	{".vert", CIncludeDirectiveParser{}},
	{".proto", ProtoIncludeDirectiveParser{}},
	{".fbs64", FlatbuffersIncludeDirectiveParser{}},
	{".gperf", CIncludeDirectiveParser{}},
	{".auxcpp", CIncludeDirectiveParser{}},
	{".pxd.pxi", CythonIncludeDirectiveParser{}},
	{".pyx.pxi", CythonIncludeDirectiveParser{}},
	{".cfgproto", CfgProtoIncludeDirectiveParser{}},
	{".gztproto", ProtoIncludeDirectiveParser{}},
})

func lookupParserForRel(rel string) IncludeDirectiveParser {
	if strings.HasSuffix(rel, ".in") {
		rel = rel[:len(rel)-len(".in")]
	}

	p, _ := parserExtMatcher.match(rel)

	return p
}
