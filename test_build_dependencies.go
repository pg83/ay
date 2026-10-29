package main

import "fmt"

type TestBuildDependency struct {
	instance ModuleInstance
	result   *ModuleEmitResult
}

func (ctx *GenCtx) testDependencyInstances(dir string) []ModuleInstance {
	if instances, ok := ctx.dependencyInstances[dir]; ok {
		return instances
	}

	instances := ctx.selectDependencyInstances(dir)

	if ctx.dependencyInstances == nil {
		ctx.dependencyInstances = map[string][]ModuleInstance{}
	}

	ctx.dependencyInstances[dir] = instances

	return instances
}

func (ctx *GenCtx) selectDependencyInstances(dir string) []ModuleInstance {
	declaration := ctx.evaluateDir(dir, ctx.target).declaration

	if declaration == nil {
		throwFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", dir)
	}

	instance := ctx.rootInstanceForDeclaration(dir, demandSelf, declaration)
	name := declaration.Name

	if isSandboxPythonTask(name) {
		variants := ctx.sandboxPythonTaskVariants(instance, name)
		instances := sandboxPythonTaskInstances(instance, name)
		selected := instances[:0]

		for _, sub := range instances {
			if variants&sandboxPythonTaskVariantBit(sub) != 0 {
				selected = append(selected, sub)
			}
		}

		return selected
	}

	frame := ctx.pushFrame()

	defer ctx.popFrame()

	if units := javaModuleUnits[name]; len(units) > 0 {
		var instances []ModuleInstance

		first := instance

		first.Kind, first.Language = units[0].sub, LangJava

		env := buildIfEnvInto(&frame.ifEnv, first)
		module := &JavaModule{dir: dir, path: first.Path, instance: first, unit: units[0], stmt: name}
		selection := collectJavaData(ctx, module, moduleStmts(ctx, dir), env)

		for _, unit := range units {
			excluded, specified := selection.excludeTags[unit.tag]

			if excluded || selection.onlyTags && !specified {
				continue
			}

			peer := instance

			peer.Kind, peer.Language = unit.sub, LangJava
			instances = append(instances, peer)
		}

		return instances
	}

	first := instance

	if name == tokPy23Library || name == tokPy23NativeLibrary || name == tokPy23Test {
		first.Language = LangPy2
	}

	if declaration.Schema {
		first.Language = LangCPP
	}

	env := buildIfEnvInto(&frame.ifEnv, first)
	d := collectModuleInto(ctx, first, moduleStmts(ctx, dir), env, ctx.onWarn, &frame.d)

	if declaration.Schema {
		var instances []ModuleInstance

		for _, unit := range []struct {
			language Language
			included bool
		}{
			{LangCPP, true},
			{LangJava, true},
			{LangPy2, true},
			{LangPy, true},
			{LangGo, true},
			{LangTS, false},
			{LangTSPrepare, false},
			{LangDescProto, true},
			{LangProtoDescriptions, true},
		} {
			excluded, specified := d.excludeTags[internStr(protoModuleTag(unit.language, true))]

			if excluded || d.onlyTags && !specified || !unit.included && !specified {
				continue
			}

			peer := instance

			peer.Language = unit.language

			if unit.language == LangJava {
				peer.Kind = KindJavaMain
			}

			instances = append(instances, peer)
		}

		return instances
	}

	instances := []ModuleInstance{instance}
	variants := d.moduleVariants

	if name == tokProtoLibrary && !moduleExcludesTag(d, "PY_PROTO") {
		variants |= 1 << LangPy2
	}

	if name == tokPy23Library || name == tokPy23NativeLibrary || name == tokPy23Test {
		variants |= 1 << LangPy2
	}

	if name == tokPackage {
		variants |= 1 << LangPackage
	}

	for _, language := range []Language{LangJava, LangPy, LangPy2, LangGo, LangDescProto, LangDocsProto, LangTSPrepare, LangTS, LangRosBindings, LangPackage} {
		if variants&(1<<language) != 0 {
			peer := instance

			peer.Language = language

			if language == LangJava {
				peer.Kind = KindJavaMain
			}

			instances = append(instances, peer)
		}
	}

	if name == tokPy3Program {
		instance.Kind = KindLib
		instances = append(instances, instance)
	}

	if name == tokYqlUdf || name == tokYqlUdfContrib {
		for _, current := range []bool{false, true} {
			peer := instance

			peer.Kind, peer.YqlCurrent = KindLib, current
			instances = append(instances, peer)
		}
	}

	selected := instances[:0]

	for _, peer := range instances {
		tag := multimoduleVariantTag(name, peer.Kind, peer.Language)

		if tag == "" {
			tag = resolveModuleUnit(name, peer.Kind, peer.Language).Tag.string()
		}

		if peer.YqlCurrent {
			tag = strYqlUdfStaticCurrent.string()
		}

		if name == tokProtoLibrary {
			tag = protoModuleTag(peer.Language, false)
		}

		if (name == tokPy23Library || name == tokPy23NativeLibrary) && peer.Language == LangPy2 {
			tag = "PY2"
		}

		excluded := moduleExcludesTag(d, tag)

		if tag != "" && excluded {
			continue
		}

		selected = append(selected, peer)
	}

	return selected
}

func (ctx *GenCtx) testBuildDependencies(dir string) []TestBuildDependency {
	if dependencies, ok := ctx.dependencyBuilds[dir]; ok {
		return dependencies
	}

	dependencies := ctx.buildTestDependencies(dir)

	if ctx.dependencyBuilds == nil {
		ctx.dependencyBuilds = map[string][]TestBuildDependency{}
	}

	ctx.dependencyBuilds[dir] = dependencies

	return dependencies
}

func (ctx *GenCtx) buildTestDependencies(dir string) []TestBuildDependency {
	var dependencies []TestBuildDependency

	final := false

	for _, instance := range ctx.testDependencyInstances(dir) {
		dependency := TestBuildDependency{instance, genModule(ctx, instance)}

		dependencies = append(dependencies, dependency)
		final = final || ctx.runtimeTestDependencyModule(dependency).final
	}

	if len(dependencies) == 0 || len(dependencies) > 1 && !final {
		ctx.onWarn(Warn{Kind: WarnBadMacroArgs, Message: fmt.Sprintf("DEPENDS(%s): directory has no selected module or multiple modules without a final target", dir)})

		return nil
	}

	seen := map[*ModuleEmitResult]bool{}
	publish := !ctx.targetDirs[dir]

	for _, dependency := range dependencies {
		ctx.testDependencyClosure(dependency, seen, true, publish)
	}

	return dependencies
}

func (ctx *GenCtx) testDependencyClosure(dependency TestBuildDependency, seen map[*ModuleEmitResult]bool, root, publish bool) {
	instance, result := dependency.instance, dependency.result

	if seen[result] {
		return
	}

	result.testDependencyModule.peers = nil

	if publish {
		for _, ref := range ctx.moduleResultRefs(instance, result) {
			ctx.emit.result(ref)
		}
	}

	module := ctx.runtimeTestDependencyModule(dependency)

	if root && module.multimodule && module.bundle && !module.final {
		return
	}

	seen[result] = true

	if jm := result.java; jm != nil {
		for _, peer := range ctx.javaManagedPeersClosure(jm) {
			if peer.java != nil {
				dependency := TestBuildDependency{peer.java.instance, peer.java.result}

				result.testDependencyModule.peers = append(result.testDependencyModule.peers, dependency)
				ctx.testDependencyClosure(dependency, seen, false, publish)
			} else if peer.foreign != nil {
				result.testDependencyModule.peers = append(result.testDependencyModule.peers,
					TestBuildDependency{peer.foreign.testDependencyModule.instance, peer.foreign})
			}
		}

		for _, peer := range jm.unmanageable {
			result.testDependencyModule.peers = append(result.testDependencyModule.peers,
				TestBuildDependency{peer.res.testDependencyModule.instance, peer.res})
		}

		return
	}

	for _, peer := range module.peerInstances {
		dependency := TestBuildDependency{peer, genModule(ctx, peer)}

		result.testDependencyModule.peers = append(result.testDependencyModule.peers, dependency)
		ctx.testDependencyClosure(dependency, seen, false, publish)
	}
}
