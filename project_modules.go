package main

func (ctx *GenCtx) setModuleOutput(reg *CodegenRegistry, owner, output VFS, ref NodeRef) {
	if output == 0 || ref == 0 {
		return
	}

	info, ok := reg.byRel.get(output.rel())

	if !ok {
		info = reg.na.geninfos.one()
		*info = GeneratedFileInfo{InputsOnly: true, OutputPath: output}
		reg.byRel.put(output.rel(), info)
	}

	info.OwnerModule = owner
	info.ProducerRef = ref
	ctx.emit.modules.add(uint32(ref))
}

func (ctx *GenCtx) publishModuleOutputs(instance ModuleInstance, result *ModuleEmitResult) {
	reg := ctx.scannerFor(instance).codegen

	if result.ARPath != nil {
		ctx.setModuleOutput(reg, instance.Path, *result.ARPath, result.ARRef)
	}

	if result.LDPath != nil {
		ctx.setModuleOutput(reg, instance.Path, *result.LDPath, result.LDRef)
	}

	if result.GlobalPath != nil && result.GlobalRef != nil {
		ctx.setModuleOutput(reg, instance.Path, *result.GlobalPath, *result.GlobalRef)
	}

	if result.java != nil {
		ctx.setModuleOutput(reg, instance.Path, result.java.target, result.java.outRef)
	}

	if inputs := reg.moduleInputs(moduleOutputPath(result)); inputs != nil {
		for _, global := range inputs.Globals {
			if info := reg.lookup(global); info != nil && info.OwnerModule == instance.Path {
				ctx.setModuleOutput(reg, instance.Path, global, info.ProducerRef)
			}
		}
	}
}

func (ctx *GenCtx) projectModuleIndex(suites []*TestSuite) map[VFS][]NodeRef {
	emit := ctx.emit
	stack := make([]NodeRef, 0, len(emit.results))

	for _, ref := range emit.results {
		node := emit.nodes.s[ref]

		if emit.stripPackagesFromResults && node != nil && node.KV != nil && (node.KV.P == pkPK || node.KV.P == pkUN) {
			continue
		}

		stack = append(stack, ref)
	}

	for _, suite := range suites {
		if ctx.testSuiteSelected(suite) {
			stack = append(stack, suite.deps...)
		}
	}

	var live BitSet

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

	projects := map[VFS][]NodeRef{}

	for _, node := range emit.nodes.s {
		if node == nil || !live.has(uint32(node.Ref)) || node.Platform == ctx.host || !node.Module && !emit.modules.has(uint32(node.Ref)) {
			continue
		}

		reg := ctx.scannerForPlatform(node.Platform).codegen

		for _, output := range node.Outputs {
			if info, _ := reg.byRel.get(output.rel()); info != nil && info.OwnerModule != 0 {
				projects[info.OwnerModule] = append(projects[info.OwnerModule], node.Ref)

				break
			}
		}
	}

	return projects
}
