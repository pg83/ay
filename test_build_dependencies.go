package main

type TestBuildDependency struct {
	instance ModuleInstance
	result   *ModuleEmitResult
}

func (ctx *GenCtx) testDependencyInstances(dir string) []ModuleInstance {
	declaration, certain := inferModuleDeclaration(moduleStmts(ctx, dir))

	if !certain {
		declaration, _ = ctx.recurseScan(dir)
	}

	if declaration == nil {
		throwFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", dir)
	}

	instance := ctx.rootInstanceForDeclaration(dir, demandSelf, declaration)
	name := declaration.Name
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
		tag := multimoduleVariantTag(name, peer.Language)

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

		if tag != "" && moduleExcludesTag(d, tag) {
			continue
		}

		selected = append(selected, peer)
	}

	return selected
}

func (ctx *GenCtx) testBuildDependencies(dir string) []TestBuildDependency {
	var dependencies []TestBuildDependency

	seen := map[*ModuleEmitResult]bool{}

	for _, instance := range ctx.testDependencyInstances(dir) {
		dependency := TestBuildDependency{instance, genModule(ctx, instance)}

		dependencies = append(dependencies, dependency)
		ctx.testDependencyClosure(dependency, seen)
	}

	return dependencies
}

func (ctx *GenCtx) testDependencyClosure(dependency TestBuildDependency, seen map[*ModuleEmitResult]bool) {
	instance, result := dependency.instance, dependency.result

	if seen[result] {
		return
	}

	seen[result] = true
	result.testDependencyModule.peers = nil

	for _, ref := range ctx.moduleResultRefs(instance, result) {
		ctx.emit.result(ref)
	}

	if jm := result.java; jm != nil {
		for _, peer := range ctx.javaManagedPeersClosure(jm) {
			if peer.java != nil {
				dependency := TestBuildDependency{peer.java.instance, peer.java.result}

				result.testDependencyModule.peers = append(result.testDependencyModule.peers, dependency)
				ctx.testDependencyClosure(dependency, seen)
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

	name := result.ModuleStmtName

	if !isUnionModule(name) && name != tokPackageFinal && name != tokCiGroup && name != tokGenLibrary && name != tokDocsLibrary {
		return
	}

	frame := ctx.pushFrame()

	defer ctx.popFrame()

	env := buildIfEnvInto(&frame.ifEnv, instance)
	d := collectModuleInto(ctx, instance, moduleStmts(ctx, instance.Path.relString()), env, ctx.onWarn, &frame.d)
	e := &EmitContext{ctx: ctx, instance: instance, d: d}

	for _, language := range d.selfPeers {
		peer := instance

		peer.Language = language

		dependency := TestBuildDependency{peer, genModule(ctx, peer)}

		result.testDependencyModule.peers = append(result.testDependencyModule.peers, dependency)
		ctx.testDependencyClosure(dependency, seen)
	}

	for _, dir := range d.peerdirs {
		peer := e.derivePeerInstance(dir.string())

		peer.Demand = demandSelf

		dependency := TestBuildDependency{peer, genModule(ctx, peer)}

		result.testDependencyModule.peers = append(result.testDependencyModule.peers, dependency)
		ctx.testDependencyClosure(dependency, seen)
	}
}
