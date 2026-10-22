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

	if units := javaModuleUnits[name]; len(units) > 0 {
		var instances []ModuleInstance

		for _, unit := range units {
			peer := instance

			peer.Kind, peer.Language = unit.sub, LangJava
			instances = append(instances, peer)
		}

		return instances
	}

	frame := ctx.pushFrame()

	defer ctx.popFrame()

	env := buildIfEnvInto(&frame.ifEnv, instance)
	d := collectModuleInto(ctx, instance, moduleStmts(ctx, dir), env, ctx.onWarn, &frame.d)
	instances := []ModuleInstance{instance}
	variants := d.moduleVariants

	if name == tokProtoLibrary && !moduleExcludesTag(d, "PY_PROTO") {
		variants |= 1 << LangPy2
	}

	if name == tokPy23Library || name == tokPy23NativeLibrary {
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
		if peer.Language != LangJava {
			env := buildIfEnvInto(&frame.ifEnv, peer)
			d := collectModuleInto(ctx, peer, moduleStmts(ctx, dir), env, ctx.onWarn, &frame.d)
			tag := multimoduleVariantTag(name, peer.Language)

			if tag == "" {
				tag = d.unit.Tag.string()
			}

			if name == tokProtoLibrary && peer.Language == LangPy2 {
				tag = "PY_PROTO"
			}

			if (name == tokPy23Library || name == tokPy23NativeLibrary) && peer.Language == LangPy2 {
				tag = "PY2"
			}

			if tag != "" && moduleExcludesTag(d, tag) {
				continue
			}
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

		if jm := dependency.result.java; jm != nil {
			excluded, specified := jm.data.excludeTags[jm.unit.tag]

			if excluded || jm.data.onlyTags && !specified {
				continue
			}
		}

		dependencies = append(dependencies, dependency)
		ctx.testDependencyClosure(dependency, seen, false)
	}

	return dependencies
}

func (ctx *GenCtx) testDependencyClosure(dependency TestBuildDependency, seen map[*ModuleEmitResult]bool, retain bool) {
	instance, result := dependency.instance, dependency.result

	if seen[result] {
		return
	}

	seen[result] = true

	for _, ref := range ctx.moduleResultRefs(instance, result) {
		if retain {
			ctx.emit.result(ref)
		}
	}

	if jm := result.java; jm != nil {
		for _, peer := range ctx.javaManagedPeersClosure(jm) {
			if peer.java != nil {
				ctx.testDependencyClosure(TestBuildDependency{peer.java.instance, peer.java.result}, seen, true)
			}
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
		ctx.testDependencyClosure(TestBuildDependency{peer, genModule(ctx, peer)}, seen, true)
	}

	for _, dir := range d.peerdirs {
		peer := e.derivePeerInstance(dir.string())

		peer.Demand = demandSelf
		ctx.testDependencyClosure(TestBuildDependency{peer, genModule(ctx, peer)}, seen, true)
	}
}
