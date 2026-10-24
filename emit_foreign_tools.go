package main

import "slices"

type foreignToolCandidate struct {
	path     ARG
	instance ModuleInstance
	target   *ModuleEmitResult
	host     *ModuleEmitResult
	boundRef NodeRef
}

type foreignToolBindings struct {
	candidates DenseMap[ARG, *foreignToolCandidate]
	byRef      DenseMap[NodeRef, *foreignToolCandidate]
	order      []*foreignToolCandidate
	startRefs  []NodeRef
	hostPaths  DenseMap[NodeRef, ARG]
}

func (e *EmitContext) foreignToolResult(path ARG) (*ModuleEmitResult, *foreignToolCandidate) {
	ctx := e.ctx

	if e.instance.Platform == ctx.host || e.instance.Platform == ctx.host.picVariant || isJavaModuleType(ctx.moduleName(path.string())) {
		return ctx.toolResult(path), nil
	}

	bindings := &ctx.foreignTools

	if candidate, ok := bindings.candidates.get(path); ok {
		return candidate.target, candidate
	}

	candidate := &foreignToolCandidate{path: path, instance: ctx.rootInstance(path.string(), demandLinked)}

	ctx.emit.deferPublication++

	defer func() { ctx.emit.deferPublication-- }()

	result := genModule(ctx, candidate.instance)

	ctx.moduleByRef.put(result.LDRef, result)

	candidate.target = result

	bindings.candidates.put(path, candidate)
	bindings.byRef.put(result.LDRef, candidate)
	bindings.order = append(bindings.order, candidate)

	return result, candidate
}

func (ctx *GenCtx) moduleBuildDeps(ref NodeRef) ([]NodeRef, bool) {
	for _, scanner := range []*IncludeScanner{ctx.scannerTarget, ctx.scannerHost, ctx.scannerTargetPIC} {
		if scanner != nil {
			if deps, ok := scanner.codegen.moduleBuildDeps.get(ref); ok {
				return deps, true
			}
		}
	}

	return nil, false
}

func (ctx *GenCtx) bindForeignTools() {
	bindings := &ctx.foreignTools

	ctx.resolveCythonSourceUses()

	if len(bindings.order) == 0 && !ctx.fixCppIncludes {
		return
	}

	for {
		ctx.resolveCythonSourceUses()

		possible := len(bindings.order)

		var visited BitSet

		stack := append([]NodeRef(nil), bindings.startRefs...)

		slices.Reverse(stack)

		for len(stack) > 0 {
			ref := stack[len(stack)-1]

			stack = stack[:len(stack)-1]

			if visited.has(uint32(ref)) {
				continue
			}

			visited.add(uint32(ref))

			node := ctx.emit.nodes.s[ref]

			if node == nil {
				continue
			}

			var selected map[NodeRef]NodeRef

			for _, dep := range node.ForeignDepRefs {
				if tool, ok := bindings.byRef.get(dep); ok {
					if tool.host == nil {
						tool.host = ctx.toolResult(tool.path)
					}

					if selected == nil {
						selected = make(map[NodeRef]NodeRef)
					}

					selected[dep] = tool.host.LDRef
				} else if path, ok := bindings.hostPaths.get(dep); ok {
					if tool, exists := bindings.candidates.get(path); exists {
						tool.host = ctx.toolResult(path)
					}
				}
			}

			if node.CppIncludeContext != nil {
				ctx.repairCppSourceInputs(node)
			}

			deps, module := ctx.moduleBuildDeps(ref)

			if !module {
				deps = node.DepRefs
			}

			if ctx.fixCppIncludes {
				deps = append([]NodeRef(nil), deps...)
				slices.Reverse(deps)
			}

			stack = append(stack, deps...)

			for _, dep := range node.ForeignDepRefs {
				if selectedRef, ok := selected[dep]; ok {
					dep = selectedRef
				}

				stack = append(stack, dep)
			}
		}

		sourcesChanged := ctx.resolveCythonSourceUses()

		if !ctx.flushFbsContextBindings() && !sourcesChanged && possible == len(bindings.order) {
			break
		}
	}

	for _, node := range ctx.emit.nodes.s {
		if node == nil {
			continue
		}

		copied := false

		for i, dep := range node.ForeignDepRefs {
			tool, ok := bindings.byRef.get(dep)

			if !ok || tool.host == nil {
				continue
			}

			if !copied {
				node.ForeignDepRefs = ctx.na.refList(node.ForeignDepRefs...)
				copied = true
			}

			selected := ctx.foreignToolRef(tool)

			node.ForeignDepRefs[i] = selected
		}

		node.foreignToolsPending = false
	}
}

func (ctx *GenCtx) stripForeignToolCandidates() {
	if len(ctx.foreignTools.order) == 0 {
		return
	}

	var candidateRoots []NodeRef

	for _, tool := range ctx.foreignTools.order {
		candidateRoots = append(candidateRoots, tool.target.LDRef)
	}

	candidateClosure := ctx.emit.reachableNodes(candidateRoots)
	roots := append([]NodeRef(nil), ctx.emit.results...)

	for _, node := range ctx.emit.nodes.s {
		if node != nil && !candidateClosure.has(uint32(node.Ref)) {
			roots = append(roots, node.Ref)
		}
	}

	live := ctx.emit.reachableNodes(roots)

	for _, node := range ctx.emit.nodes.s {
		if node != nil && candidateClosure.has(uint32(node.Ref)) && !live.has(uint32(node.Ref)) {
			ctx.emit.nodes.s[node.Ref] = nil
		}
	}
}

func (e *StreamingEmitter) reachableNodes(roots []NodeRef) BitSet {
	var visited BitSet

	stack := append([]NodeRef(nil), roots...)

	for len(stack) > 0 {
		ref := stack[len(stack)-1]

		stack = stack[:len(stack)-1]

		if visited.has(uint32(ref)) {
			continue
		}

		visited.add(uint32(ref))

		if node := e.nodes.s[ref]; node != nil {
			for dep := range node.buildDeps(e.fetchRefs) {
				stack = append(stack, dep)
			}
		}
	}

	return visited
}

func (ctx *GenCtx) foreignToolRef(tool *foreignToolCandidate) NodeRef {
	if tool.boundRef != 0 {
		return tool.boundRef
	}

	if *tool.target.LDPath == *tool.host.LDPath {
		return tool.host.LDRef
	}

	from, to := *tool.host.LDPath, *tool.target.LDPath

	tool.boundRef = ctx.emit.emitNode(Node{
		Platform: ctx.host,
		Cmds: ctx.na.cmdList(Cmd{
			CmdArgs: ctx.na.chunkList(ctx.na.anyList(strBinMv.any(), from.any(), to.any())),
			Cwd:     bldRootDirVFS,
		}),
		Inputs:  ctx.na.inputList(ctx.na.vfsList(from)),
		Outputs: ctx.na.vfsList(to),
		DepRefs: ctx.na.refList(tool.host.LDRef),
		KV:      &KV{P: pkCP, PC: pcLightBlue},
	})

	return tool.boundRef
}
