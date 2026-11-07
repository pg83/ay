package main

var (
	strResourcePatternClang18          = internStr(resourcePatternClang18)
	strResourcePatternOSSDKRoot        = internStr(resourcePatternOSSDKRoot)
	bldResourcesYMAKEPYTHON3BinPython3 = build("resources/", resourcePatternYMakePython3, "/bin/python3")
)

const (
	resourcePatternYMakePython3 = "YMAKE_PYTHON3"
	resourcePatternClang14      = "CLANG14"
	resourcePatternClang16      = "CLANG16"
	resourcePatternClang18      = "CLANG18"
	resourcePatternClang20      = "CLANG20"
	resourcePatternClangFormat  = "CLANG_FORMAT"
	resourcePatternClangTool    = "CLANG"
	resourcePatternLLDRoot      = "LLD_ROOT"
	resourcePatternJDK17        = "JDK17"
	resourcePatternOSSDKRoot    = "OS_SDK_ROOT"
)
