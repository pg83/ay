package main

import (
	"encoding/json"
	"path"
	"strings"
	"testing"
)

type ContractFS struct {
	FS
	files map[string]string
}

func (fs ContractFS) isFile(prefix STR, suffix string) bool {
	_, ok := fs.files[path.Join(prefix.string(), suffix)]

	return ok
}

func (fs ContractFS) exists(prefix STR, suffix string) (bool, bool) {
	return fs.isFile(prefix, suffix), false
}

func (fs ContractFS) isFilePathClean(rel string) bool {
	_, ok := fs.files[rel]

	return ok
}

func (fs ContractFS) read(rel string) [][]byte {
	return [][]byte{[]byte(fs.files[rel])}
}

func (fs ContractFS) listdir(dir STR) DirView {
	return DirView{dir: dir, names: []uint32{}}
}

func TestToolchainConfiguration(t *testing.T) {
	fs := ContractFS{files: map[string]string{yaConfJSON: `{"unrelated":[1,2],"toolchain":{"skip":{},"clang":{"name":"custom","tools":{"c++":{"executable":"clang++"}},"platforms":[{"host":{"os":"linux"}}]}}}`}}
	cfg := readYaConfToolchains(fs)
	tc := resolveCxxToolchain(fs, cfg, "CUSTOM-LINUX-X86_64", "CUSTOM-LINUX-X86_64")

	testEqual(t, tc.Name, "clang")
	testEqual(t, tc.ToolVar, "")
	testEqual(t, tc.Params["bottle_name"], any(nil))
	testEqual(t, tc.Params["env"], any(map[string]any{}))
	fallback := map[string]any{"os": "linux"}
	copy := toolchainPlatform("test", nil, fallback)
	copy["os"] = "darwin"
	testEqual(t, fallback["os"], any("linux"))
	target := map[string]any{"toolchain": "custom", "os": "linux", "arch": "aarch64"}
	tc = newToolchain(fs, cfg, "custom", map[string]any{"params": map[string]any{"match_root": "ROOT"}}, map[string]any{}, target, target)
	testEqual(t, tc.ToolVar, "CUSTOM_LINUX_AARCH64")
	testEqual(t, tc.MatchRoot, "ROOT")
	testEqual(t, canonizeResourcePlatform("ANY"), "any")
	testEqual(t, extendConfBuildType("dist-release"), "dist-release")
}

func TestToolchainMalformedConfiguration(t *testing.T) {
	testThrows(t, "want", func() { expectJSONDelim(json.NewDecoder(strings.NewReader("[]")), '{') })
	testThrows(t, "object key is not a string", func() { jsonKey(json.NewDecoder(strings.NewReader("42"))) })
	testThrows(t, "lacks a host platform", func() { toolchainPlatform("broken", nil, nil) })
	testThrows(t, "is missing", func() { toolchainFormulaValue(ContractFS{}, "missing.json") })
	testThrows(t, "no 'by_platform' mapping", func() { mappingVarName("ROOT", nil) })
	testThrows(t, "unsupported resource platform", func() { canonizeResourcePlatform("unknown") })
}

func TestDocsUnicodeAndGlobs(t *testing.T) {
	testEqual(t, docsJSONString("Привет 😀 <&>"), `"\u041f\u0440\u0438\u0432\u0435\u0442 \ud83d\ude00 <&>"`)

	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"**", "a/b", true}, {"**/*.md", "a.md", true},
		{"**/*.md", "a/b.md", true}, {"**/*.md", "a/b.txt", false},
	} {
		testEqual(t, docsGlobMatches(tc.pattern, tc.value), tc.want)
	}

	d := &DocsData{}
	testEqual(t, d.apply(UnknownStmt{}, "docs"), false)
	testThrows(t, "missing value", func() { d.apply(UnknownStmt{Name: tokDocsCopyFiles, Args: internAnys([]string{"FROM"})}, "docs") })
	testThrows(t, "missing SRCDIR", func() { d.addSources(nil, "docs", []string{"SRCDIR"}) })
	d = &DocsData{explicitSources: true, vars: []string{"missing-equals"}}
	testThrows(t, "expected name=value", func() { d.finish(ContractFS{}, "docs", newEnvironment()) })
}

func TestRosSourceValidation(t *testing.T) {
	for _, tc := range []struct {
		name TOK
		args []string
		want string
	}{
		{tokRosDir, nil, "expects one directory"},
		{tokRosPartial, nil, "expects one name"},
		{tokRosSrcs, []string{"PARTIAL"}, "Expected library name"},
		{tokRosSrcs, []string{"file.txt"}, "unsupported ROS file"},
		{tokRosSrcs, []string{"ZERO_COPY", "file.srv"}, "ZERO_COPY for .srv"},
	} {
		testThrows(t, tc.want, func() {
			applyRosStmt("pkg", UnknownStmt{Name: tc.name, Args: internAnys(tc.args)}, newEnvironment(), &ModuleData{ros: &RosModule{}})
		})
	}

	applyRosStmt("pkg", UnknownStmt{}, newEnvironment(), &ModuleData{})
	fs := ContractFS{files: map[string]string{"messages/Point.msg": "float64 x"}}
	testEqual(t, resolveRosInclude(fs, nil, "Point"), VFS(0))
	testEqual(t, resolveRosInclude(fs, []VFS{source("pkg:messages")}, "Point"), source("messages/Point.msg"))
	testEqual(t, resolveRosInclude(fs, []VFS{source("pkg:messages")}, "Missing"), VFS(0))
	testEqual(t, resolveRosInclude(fs, []VFS{source("pkg:messages")}, "other/Point"), VFS(0))
}

func TestInvalidModuleMacroArguments(t *testing.T) {
	for _, tc := range []struct {
		name TOK
		args []string
		want string
	}{
		{tokGenPchFile, nil, "expects an input header"},
		{tokDebVersion, nil, "expects one changelog"},
		{tokYaToolsConf, nil, "expects one directory"},
		{tokLlvmCompileCxx, nil, "requires input, output"},
		{tokCompileLua, nil, "expects a source"},
		{tokBundleOutput, nil, "expects a target"},
		{tokGoEmbedDir, nil, "expects a directory"},
		{tokTsProtoPackageName, nil, "expects one package name"},
		{tokSetCustomClangTidy, nil, "expects a resource module"},
		{tokYqlCurrentAbiVersion, []string{"extra"}, "expects no arguments"},
		{tokFbsNamespace, nil, "expects a namespace"},
		{tokProto2Fbs, nil, "expects one proto input"},
		{tokLinkerScript, []string{"link.ld"}, "outside a PROGRAM"},
		{tokTestSrcs, []string{"NAMESPACE"}, "is not allowed"},
		{tokSetListPreSuf, nil, "expects an output"},
	} {
		t.Run(tc.name.string(), func(t *testing.T) {
			d := &ModuleData{moduleStmt: &ModuleStmt{Name: tokLibrary}}

			testThrows(t, tc.want, func() {
				applyUnknownStmt(nil, "app", UnknownStmt{Name: tc.name, Args: internAnys(tc.args)}, d, newEnvironment(), nil)
			})
		})
	}
}

func TestGoEmbedBindirValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "expects a pattern"}, {[]string{"PREFIX"}, "requires a path"}, {[]string{"a", "b"}, "expects one pattern"},
	} {
		testThrows(t, tc.want, func() { newGoEmbedBindir(false, "pkg", internAnys(tc.args)) })
	}

	em := newGoEmbedBindir(true, "pkg", internAnys([]string{"*.txt", "PREFIX", "gen", "FILES", "a.txt"}))
	args, inputs := goEmbedArgs(newNodeArenas(), []GoEmbed{em})
	testEqual(t, anyStrs(args), []string{"++embed_xtest", "$(B)/gen", "*.txt", "$(B)/gen/a.txt"})
	testEqual(t, inputs, []VFS{build("gen/a.txt")})
}
