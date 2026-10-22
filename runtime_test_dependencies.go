package main

import (
	"path"
	"slices"
	"strings"
)

type TestDependencyModule struct {
	instance     ModuleInstance
	bundle       bool
	includePeers bool
	final        bool
	multimodule  bool
	fake         bool
	peers        []TestBuildDependency
}

func (e *EmitContext) recordTestDependencyModule(result *ModuleEmitResult, declaration TOK, env Environment) {
	d := e.d
	name := d.unit.Type
	bundle := isUnionModule(name) || name == tokPackageFinal || name == tokCiGroup || name == tokGenLibrary || name == tokGoLibrary || name == tokProtoDescriptions || name == tokProtoRegistry || e.instance.Language == LangDescProto || d.nots != nil
	includePeers := isUnionModule(name) || name == tokGenLibrary || e.instance.Language == LangDescProto
	multimodule := d.multimodule != 0 || declaration == tokProtoLibrary || declaration == tokPy3Program || declaration == tokPy3Test || declaration == tokPy23Test || isPythonMultimodule(declaration) || declaration == tokYqlUdf || declaration == tokYqlUdfContrib
	final := (isLinkingModuleType(name) || name == tokProtoDescriptions || name == tokProtoRegistry || name == tokDocs) && name != tokExectest && name != tokPy2Test && name != tokPytestBin

	if (declaration == tokYqlUdf || declaration == tokYqlUdfContrib) && e.instance.Kind == KindLib {
		final = false
	}

	if name == tokPackageFinal || name == tokCiGroup || d.nots != nil && e.instance.Language != LangTSPrepare {
		final = true
	}

	result.testDependencyModule = TestDependencyModule{instance: e.instance, bundle: bundle, includePeers: includePeers, final: final, multimodule: multimodule,
		fake: !bundle && !isLinkingModuleType(name) && env.bool(envFakeModule)}
}

func (ctx *GenCtx) runtimeTestDependencyModule(dependency TestBuildDependency) TestDependencyModule {
	result := dependency.result

	if jm := result.java; jm != nil {
		return TestDependencyModule{bundle: true, includePeers: true, final: jm.unit.finalTarget(), multimodule: len(javaModuleUnits[jm.stmt]) > 1 || jm.unit.proto || jm.unit.fbs, peers: result.testDependencyModule.peers}
	}

	return result.testDependencyModule
}

func testDependencyOutput(result *ModuleEmitResult) VFS {
	if result.java != nil {
		return result.java.target
	}

	if result.LDPath != nil {
		return *result.LDPath
	}

	return 0
}

func (ctx *GenCtx) runtimeTestDependencies(dir string) []TestBuildDependency {
	roots := ctx.testBuildDependencies(dir)

	var artifacts []TestBuildDependency

	seen := map[*ModuleEmitResult]bool{}

	var visit func(TestBuildDependency, bool)

	visit = func(dependency TestBuildDependency, root bool) {
		result := dependency.result
		module := ctx.runtimeTestDependencyModule(dependency)

		if seen[result] || root && module.multimodule && module.bundle && !module.final {
			return
		}

		seen[result] = true

		if (!root || !module.multimodule || module.final) && !module.fake && testDependencyOutput(result) != 0 {
			artifacts = append(artifacts, dependency)
		}

		if module.bundle && module.includePeers {
			for _, peer := range module.peers {
				visit(peer, false)
			}
		}
	}

	for _, dependency := range roots {
		visit(dependency, true)
	}

	slices.SortFunc(artifacts, func(a, b TestBuildDependency) int {
		return strings.Compare(testDependencyOutput(a.result).string(), testDependencyOutput(b.result).string())
	})

	return slices.CompactFunc(artifacts, func(a, b TestBuildDependency) bool {
		return testDependencyOutput(a.result) == testDependencyOutput(b.result)
	})
}

func (ctx *GenCtx) runtimeTestOutputDirs(dependency TestBuildDependency, seen map[*ModuleEmitResult]bool) []string {
	result := dependency.result

	if seen[result] {
		return nil
	}

	seen[result] = true

	if result.LDPath != nil && result.ARPath == nil && isLinkingModuleType(result.ModuleStmtName) && result.ModuleStmtName != tokDllTool {
		return []string{path.Dir(result.LDPath.string())}
	}

	var dirs []string

	if module := ctx.runtimeTestDependencyModule(dependency); module.bundle {
		for _, peer := range module.peers {
			dirs = append(dirs, ctx.runtimeTestOutputDirs(peer, seen)...)
		}
	}

	return dirs
}
