package main

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
)

func resourcePatternRef(pattern string) string {
	return "$(" + pattern + ")"
}

func resourceGlobalRef(name, pattern string) string {
	return name + "::" + resourcePatternRef(pattern)
}
