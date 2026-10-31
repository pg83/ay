package main

func (ctx *GenCtx) setModuleOutput(reg *CodegenRegistry, owner, output VFS, ref NodeRef, moduleType TOK) {
	if output == 0 || ref == 0 {
		return
	}

	info, ok := reg.byRel.get(output.rel())

	if !ok {
		info = reg.na.geninfos.one()
		reg.store(info, GeneratedFileSpec{InputsOnly: true, OutputPath: output})
		reg.byRel.put(output.rel(), info)
	}

	if info.ModuleType != 0 && info.OwnerModule != 0 && info.OwnerModule != owner {
		return
	}

	if moduleType != 0 {
		info.ModuleType = moduleType
	}

	info.OwnerModule = owner
	info.ProducerRef = ref
	reg.noteOwner(info)
	ctx.emit.modules.add(uint32(ref))
}

func (ctx *GenCtx) publishModuleOutputs(instance ModuleInstance, result *ModuleEmitResult) {
	reg := ctx.scannerFor(instance).codegen

	if result.ARPath != nil {
		ctx.setModuleOutput(reg, instance.Path, *result.ARPath, result.ARRef, result.ModuleStmtName)
	}

	if result.LDPath != nil {
		ctx.setModuleOutput(reg, instance.Path, *result.LDPath, result.LDRef, result.ModuleStmtName)
	}

	if result.GlobalPath != nil && result.GlobalRef != nil {
		ctx.setModuleOutput(reg, instance.Path, *result.GlobalPath, *result.GlobalRef, 0)
	}

	if result.java != nil {
		ctx.setModuleOutput(reg, instance.Path, result.java.target, result.java.outRef, result.ModuleStmtName)
	}

	if inputs := reg.moduleInputs(moduleOutputPath(result)); inputs != nil {
		for _, global := range inputs.Globals {
			if info := reg.lookup(global); info != nil && info.OwnerModule == instance.Path {
				ctx.setModuleOutput(reg, instance.Path, global, info.ProducerRef, 0)
			}
		}
	}
}

func (ctx *GenCtx) projectModuleIndex(suites []*TestSuite) (map[VFS][]NodeRef, map[VFS][]NodeRef) {
	emit := ctx.emit

	var live BitSet
	var stack []NodeRef

	mark := func(root NodeRef) {
		if live.has(uint32(root)) {
			return
		}

		stack = append(stack[:0], root)

		for len(stack) > 0 {
			ref := stack[len(stack)-1]

			stack = stack[:len(stack)-1]

			node := emit.nodes.s[ref]

			if live.has(uint32(ref)) || node == nil {
				continue
			}

			live.add(uint32(ref))

			for dependency := range node.buildDeps(emit.fetchRefs) {
				stack = append(stack, dependency)
			}
		}
	}

	for _, ref := range emit.results {
		mark(ref)
	}

	for _, suite := range suites {
		if ctx.testSuiteSelected(suite) {
			for _, ref := range suite.deps {
				mark(ref)
			}
		}
	}

	projects := map[VFS][]NodeRef{}
	mainProjects := map[VFS][]NodeRef{}
	eligibleProjects := map[VFS]map[NodeRef]bool{}

	for _, node := range emit.nodes.s {
		if node == nil || !live.has(uint32(node.Ref)) || node.Platform == ctx.host || !node.Module && !emit.modules.has(uint32(node.Ref)) {
			continue
		}

		reg := ctx.scannerForPlatform(node.Platform).codegen

		for _, output := range node.Outputs {
			if info, _ := reg.byRel.get(output.rel()); info != nil && info.OwnerModule != 0 {
				projects[info.OwnerModule] = append(projects[info.OwnerModule], node.Ref)

				if info.ModuleType != 0 {
					eligible, ready := eligibleProjects[info.OwnerModule]

					if !ready {
						eligible = ctx.testDependencyMainRefs(info.OwnerModule)
						eligibleProjects[info.OwnerModule] = eligible
					}

					if eligible[node.Ref] {
						mainProjects[info.OwnerModule] = append(mainProjects[info.OwnerModule], node.Ref)
					}
				}

				break
			}
		}
	}

	return projects, mainProjects
}
