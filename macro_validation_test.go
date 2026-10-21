package main

import (
	"slices"
	"strings"
	"testing"
)

func TestCompilerFlagModes(t *testing.T) {
	env := newEnvironment()
	testEqual(t, len(optimizeFlags(env)), 0)
	env.setString(envBuildRelease, "yes")
	testEqual(t, anyStrs(optimizeFlags(env)), []string{"-O3"})
	env.setString(envBuildSizeOptimized, "yes")
	testEqual(t, anyStrs(optimizeFlags(env)), []string{"-Os"})
	env.setString(envCLANG, "yes")
	testEqual(t, anyStrs(optimizeFlags(env)), []string{"-Oz"})
	testThrows(t, "compiler flag $UNKNOWN", func() { expandCompilerConfFlags(env, "$UNKNOWN") })
	testThrows(t, "unsupported platform ISA", func() { compileFlagBundleFor(&Platform{ISA: "invalid"}) })
}

func TestImplibValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "requires a library name and path"},
		{[]string{"lib", "path", "SONAME"}, "requires a value"},
		{[]string{"lib", "path", "UNKNOWN"}, "unexpected argument"},
	} {
		testThrows(t, tc.want, func() { parseImplib(internAnys(tc.args)) })
	}
}

func TestMlaunchValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "keyword/value pairs"}, {[]string{"builder", "UNKNOWN", "x"}, "Unknown NODE_BUILDER"},
		{[]string{"builder", "NAME", "x", "NAME", "y"}, "Repeated NODE_BUILDER"}, {[]string{"builder"}, "requires NAME"},
	} {
		testThrows(t, tc.want, func() { checkNodeBuilderOptions(internAnys(tc.args)) })
	}
	for _, name := range []TOK{tokGolaunchFunction, tokGolaunchOverride, tokGenerateGolaunchCallbacks} {
		testThrows(t, "expects", func() { applyMlaunchStmt("pkg", UnknownStmt{Name: name}, newEnvironment(), &ModuleData{}) })
	}
	d := &ModuleData{unit: ModuleUnit{Type: tokGoLibrary}}
	applyMlaunchStmt("pkg", UnknownStmt{Name: tokNoGolaunchSrcs}, newEnvironment(), d)
	testEqual(t, d.mlaunch.Functions, []string{"pkg:Launch"})
	testEqual(t, d.mlaunch.Generate, "component")
}

func TestProtoGeneratedIncludeDirectories(t *testing.T) {
	d := &ModuleData{}
	applyProtoAddIncl(d, internAnys([]string{"GLOBAL", "WITH_GEN", "api/../proto"}))
	testEqual(t, d.protoAddInclGlobal, []VFS{build("proto"), source("proto")})
	testEqual(t, d.addInclGlobal, []VFS{build("proto")})
	testEqual(t, d.addInclUserGlobal, []VFS{build("proto")})
}

func TestExplicitStyleValidation(t *testing.T) {
	d := &ModuleData{moduleStmt: &ModuleStmt{Name: tokPy2Library}}
	env := newEnvironment()
	d.collectExplicitPythonStyle(UnknownStmt{Name: tokStylePython}, env)
	testEqual(t, len(d.styleChecks), 0)
	d.moduleStmt.Name = tokPy3Library
	for _, tc := range []struct{ arg, want string }{{"CONFIG_TYPE", "requires a config type"}, {"CHECK_FORMAT", "requires ruff"}, {"RUN_IN_SOURCE_ROOT", "requires ruff"}} {
		testThrows(t, tc.want, func() {
			d.collectExplicitPythonStyle(UnknownStmt{Name: tokStylePython, Args: internAnys([]string{tc.arg})}, env)
		})
	}
	for _, mode := range []struct {
		key   ENV
		value string
	}{{internEnv("NO_LINT"), "none"}, {internEnv("NO_LINT"), "none_internal"}, {internEnv("TIDY"), "yes"}} {
		e := newEnvironment()
		e.setString(mode.key, mode.value)
		d.collectYAMLStyleWithConfig("app", e)
		d.collectStyleCheck(nil, "app", UnknownStmt{Name: tokStyleYAML}, e)
	}
	testEqual(t, len(d.styleChecks), 0)
	env.setString(internEnv("YAML_LINT_SRCS"), "a.yaml")
	testThrows(t, "over YAML_LINT_SRCS", func() { d.collectYAMLStyleWithConfig("app", env) })
}

func TestYqlPythonRegistrationOptions(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"NAME"}, "requires a value"}, {[]string{"UNKNOWN", "value"}, "unknown option"},
		{[]string{"ADD_LIBRA_MODULES", "yes"}, "not supported in opensource"},
	} {
		env := newEnvironment()
		env.setString(envOPENSOURCE, "yes")
		testThrows(t, tc.want, func() {
			registerYqlPythonUdf(ContractFS{}, "app", UnknownStmt{Args: internAnys(tc.args)}, &ModuleData{}, env, nil)
		})
	}
	d := &ModuleData{}
	registerYqlPythonUdf(ContractFS{}, "app", UnknownStmt{Args: internAnys([]string{"ADD_LIBRA_MODULES", "yes"})}, d, newEnvironment(), nil)
	testEqual(t, slices.Contains(anyStrs(d.peerdirs), "quality/user_sessions/libra_arc/noyql"), true)
	testEqual(t, slices.Contains(anyStrs(d.peerdirs), "yql/udfs/quality/libra/module"), true)
}

func TestCppTestModuleOptions(t *testing.T) {
	env := newEnvironment()
	env.setString(internEnv("USE_AFL"), "yes")
	d := &ModuleData{}
	applyCppTestModule(&ModuleStmt{Name: tokFuzz}, d, env)
	testEqual(t, slices.Contains(anyStrs(d.peerdirs), "contrib/libs/afl"), true)
	env.setString(internEnv("UT_SKIP_EXCEPTIONS"), "yes")
	d = &ModuleData{}
	applyCppTestModule(&ModuleStmt{Name: tokUnittest}, d, env)
	testEqual(t, anyStrs(d.cFlags), []string{strDutSkipExceptions.string()})
	testEqual(t, fullPathTestName("parent/"+strings.Repeat("x", 70)), strings.Repeat("x", 70))
}

func TestModuleOptionsPersist(t *testing.T) {
	fs := ContractFS{files: map[string]string{"build/internal/platform/cuda/ya.make": ""}}
	env := newEnvironment()
	d := &ModuleData{moduleStmt: &ModuleStmt{Name: tokGoTest}, useLightPy2cc: true}
	apply := func(name TOK, args ...string) {
		applyUnknownStmt(fs, "app", UnknownStmt{Name: name, Args: internAnys(args)}, d, env, nil)
	}
	apply(tokGoXtestSrcs, "external_test.go")
	testEqual(t, anyStrs(d.goXTestSrcs), []string{"external_test.go"})
	apply(tokNoLint, "ktlint")
	testEqual(t, d.noLint, "ktlint")
	apply(tokDisable, "USE_LIGHT_PY2CC")
	testEqual(t, d.useLightPy2cc, false)
	env.setString(internEnv("DISABLE_BUILD"), "yes")
	apply(tokNoBuildIf, "WARNING", "DISABLE_BUILD")
	testEqual(t, d.discarded, false)
	apply(tokLinkExcludeLibraries, "unused")
	testEqual(t, anyStrs(d.linkExcludeLibraries), []string{"unused"})
	apply(tokCudaSrcs, "kernels.cu")
	testEqual(t, d.cudaSrcs[0].Architectures, "sm_50")
	apply(tokNvccDeviceLink, "kernels.cu")
	testEqual(t, slices.Contains(anyStrs(d.peerdirs), "build/internal/platform/cuda"), true)
	d.moduleStmt.Name = tokProgram
	testThrows(t, "only .ld scripts", func() { apply(tokLinkerScript, "invalid.txt") })
	d.setVars = nil
	apply(tokSetListPreSuf, "OUTPUT", "pre", ":suf", "a", "b")
	testEqual(t, d.setVars[internStr("OUTPUT")].string(), "prea:suf preb:suf")
}

func TestGoGatewayNonGoPeers(t *testing.T) {
	for _, tc := range []struct {
		name TOK
		peer string
	}{{tokGoGrpcGatewayV2OpenapiSrcs, goGatewayV2Options}, {tokGoGrpcGatewaySwagger, goGatewayV1Options}} {
		d := &ModuleData{unit: ModuleUnit{Type: tokLibrary}}
		applyGoGrpcGatewayStmt(UnknownStmt{Name: tc.name, Args: internAnys([]string{"api.proto"})}, d, newEnvironment())
		testEqual(t, anyStrs(d.peerdirs), []string{tc.peer})
		testEqual(t, len(d.goCodegens), 0)
	}
}
