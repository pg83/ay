package main

import "strings"

const (
	resourcePatternYMakePython3 = "YMAKE_PYTHON3-1002064631"
	resourcePatternClang14      = "CLANG14-1922233694"
	resourcePatternClang16      = "CLANG16-1380963495"
	resourcePatternClang18      = "CLANG18-1866954364"
	resourcePatternClang20      = "CLANG20-178457234"
	resourcePatternClangFormat  = "CLANG_FORMAT-2463648791"
	resourcePatternClangTool    = "CLANG-1274503668"
	resourcePatternLLDRoot      = "LLD_ROOT-3107549726"
	resourcePatternJDK17        = "JDK17-564746473"
)

func resourcePatternRef(pattern string) string {
	return "$(" + pattern + ")"
}

func resourceGlobalRef(name, pattern string) string {
	return name + "::" + resourcePatternRef(pattern)
}

var resourcePatternReplacer = strings.NewReplacer(
	"$(YMAKE_PYTHON3)", resourcePatternRef(resourcePatternYMakePython3),
	"$(CLANG14)", resourcePatternRef(resourcePatternClang14),
	"$(CLANG16)", resourcePatternRef(resourcePatternClang16),
	"$(CLANG18)", resourcePatternRef(resourcePatternClang18),
	"$(CLANG20)", resourcePatternRef(resourcePatternClang20),
	"$(CLANG_FORMAT)", resourcePatternRef(resourcePatternClangFormat),
	"$(CLANG)", resourcePatternRef(resourcePatternClangTool),
	"$(LLD_ROOT)", resourcePatternRef(resourcePatternLLDRoot),
	"$(JDK17)", resourcePatternRef(resourcePatternJDK17),
)

func canonicalizeResourcePatternRefs(s string) string {
	return resourcePatternReplacer.Replace(s)
}
