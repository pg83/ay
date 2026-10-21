package main

var (
	cParser      = CIncludeDirectiveParser{}
	goCgoParser  = GoCgoIncludeDirectiveParser{}
	emptyParser  = EmptyIncludeDirectiveParser{}
	ragelParser  = RagelIncludeDirectiveParser{}
	fbsParser    = FlatbuffersIncludeDirectiveParser{}
	yasmParser   = YasmIncludeDirectiveParser{}
	cythonParser = CythonIncludeDirectiveParser{}
	swigParser   = SwigIncludeDirectiveParser{}
	lexParser    = LexIncludeDirectiveParser{}
	tsParser     = TSIncludeDirectiveParser{}
)

func buildParserExtMatcher(proto ProtoIncludeDirectiveParser) *ExtMatcher[IncludeDirectiveParser] {
	return newExtMatcher([]ExtEntry[IncludeDirectiveParser]{
		{".msg", RosIncludeDirectiveParser{}},
		{".srv", RosIncludeDirectiveParser{}},
		{".y", lexParser},
		{".l", lexParser},
		{".a", emptyParser},
		{".f", FortranIncludeDirectiveParser{}},
		{".m", cParser},
		{".C", cParser},
		{".S", cParser},
		{".c", cParser},
		{".s", cParser},
		{".H", cParser},
		{".h", cParser},
		{".go", goCgoParser},
		{".ts", tsParser},
		{".js", tsParser},
		{".tsx", tsParser},
		{".jsx", tsParser},
		{".g4", emptyParser},
		{".mm", cParser},
		{".xh", cParser},
		{".hh", cParser},
		{".py", emptyParser},
		{".sh", emptyParser},
		{".rl", ragelParser},
		{".cc", cParser},
		{".m4", emptyParser},
		{".cu", cParser},
		{".ev", proto},
		{".rh", ragelParser},
		{".fbs", fbsParser},
		{".asi", yasmParser},
		{".pyx", cythonParser},
		{".pyi", emptyParser},
		{".fml", emptyParser},
		{".pln", emptyParser},
		{".lua", emptyParser},
		{".bin", emptyParser},
		{".stg", emptyParser},
		{".swg", swigParser},
		{".nlg", NlgIncludeDirectiveParser{}},
		{".ypp", cParser},
		{".rli", ragelParser},
		{".rl5", ragelParser},
		{".pxi", cythonParser},
		{".rl6", ragelParser},
		{".inl", cParser},
		{".gzt", proto},
		{".asm", yasmParser},
		{".lex", lexParser},
		{".pxd", cythonParser},
		{".cpp", cParser},
		{".asp", lexParser},
		{".cxx", cParser},
		{".hpp", cParser},
		{".ipp", cParser},
		{".hxx", cParser},
		{".ixx", cParser},
		{".lpp", cParser},
		{".cuh", cParser},
		{".sfdl", cParser},
		{".fml2", emptyParser},
		{".fml3", emptyParser},
		{".info", emptyParser},
		{".geom", cParser},
		{".tesc", cParser},
		{".comp", cParser},
		{".tese", cParser},
		{".frag", cParser},
		{".vert", cParser},
		{".proto", proto},
		{".fbs64", fbsParser},
		{".gperf", lexParser},
		{".pysrc", emptyParser},
		{".auxcpp", cParser},
		{".pxd.pxi", cythonParser},
		{".pyx.pxi", cythonParser},
		{".cfgproto", CfgProtoIncludeDirectiveParser{proto: proto}},
		{".gztproto", proto},
	})
}
