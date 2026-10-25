package main

import (
	"fmt"
	"slices"
)

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
	candidate, _ := bindings.candidates.get(path)

	if candidate != nil && candidate.target != nil {
		ctx.consumeModuleWarnings(candidate.target)

		return candidate.target, candidate
	}

	if candidate == nil {
		candidate = &foreignToolCandidate{path: path}
	}

	candidate.instance = ctx.rootInstance(path.string(), demandLinked)

	result := func() *ModuleEmitResult {
		ctx.emit.deferPublication++

		defer func() { ctx.emit.deferPublication-- }()

		return genModule(ctx, candidate.instance)
	}()

	ctx.consumeModuleWarnings(result)

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

func (ctx *GenCtx) activateNodeInputs(node *Node, activation map[*IncludeScanner]*BitSet, selectProducer func(NodeRef), modulePeers bool) {
	plan := &node.Inputs

	if !modulePeers && plan.pending != nil {
		pending := plan.pending

		plan.pending = nil
		pending.fire()
	}

	if plan.scanner == nil {
		return
	}

	seen := activation[plan.scanner]

	if seen == nil {
		seen = &BitSet{}
		activation[plan.scanner] = seen
	}

	registry := plan.scanner.codegen
	buildDeps, module := registry.moduleBuildDeps.get(node.Ref)

	module = module && !modulePeers

	var added []NodeRef

	plan.activate(seen, func(ref NodeRef) {
		selectProducer(ref)

		if ref == node.Ref || slices.Contains(node.DepRefs, ref) && (!module || slices.Contains(buildDeps, ref)) {
			return
		}

		if added == nil {
			added = nodeRefScratches.get()
		}

		added = append(added, ref)
	}, modulePeers)

	if len(added) == 0 {
		return
	}

	refs := nodeRefScratches.get()

	refs = append(refs, node.DepRefs...)
	refs = append(refs, added...)

	node.DepRefs = ctx.na.uniqueRefs(refs)

	if module {
		refs = append(refs[:0], buildDeps...)
		refs = append(refs, added...)
		registry.moduleBuildDeps.put(node.Ref, ctx.na.uniqueRefs(refs))
	}

	nodeRefScratches.put(refs)
	nodeRefScratches.put(added)
}

func (ctx *GenCtx) bindForeignTools() {
	bindings := &ctx.foreignTools
	activation := map[*IncludeScanner]*BitSet{}
	weak := nodeRefScratches.get()

	defer func() { nodeRefScratches.put(weak) }()

	var selected BitSet

	for {
		possible := len(bindings.order)

		selected = BitSet{}
		weak = weak[:0]

		stack := append([]NodeRef(nil), bindings.startRefs...)

		selectProducer := func(ref NodeRef) {
			stack = append(stack, ref)
		}

		for len(stack) > 0 {
			ref := stack[len(stack)-1]

			stack = stack[:len(stack)-1]

			if selected.has(uint32(ref)) {
				continue
			}

			node := ctx.emit.nodes.s[ref]

			if node == nil {
				continue
			}

			selected.add(uint32(ref))
			ctx.activateNodeInputs(node, activation, selectProducer, false)

			if len(node.Inputs.modulePeers) > 0 {
				weak = append(weak, ref)
			}

			var foreign map[NodeRef]NodeRef

			for _, dep := range node.ForeignDepRefs {
				if tool, ok := bindings.byRef.get(dep); ok {
					if tool.host == nil {
						tool.host = ctx.toolResult(tool.path)
					}

					if foreign == nil {
						foreign = make(map[NodeRef]NodeRef)
					}

					foreign[dep] = tool.host.LDRef
				} else if path, ok := bindings.hostPaths.get(dep); ok {
					tool, exists := bindings.candidates.get(path)

					if !exists {
						tool = &foreignToolCandidate{path: path}
						bindings.candidates.put(path, tool)
					}

					tool.host = ctx.toolResult(path)
				}
			}

			deps, module := ctx.moduleBuildDeps(ref)

			if !module {
				deps = node.DepRefs
			} else if !slices.Equal(deps, node.DepRefs) {
				dedupers.with(func(seen *DeDuper) {
					for _, dep := range deps {
						seen.addStable(dep.strID())
					}

					for _, dep := range node.DepRefs {
						if seen.addStable(dep.strID()) {
							weak = append(weak, dep)
						}
					}
				})
			}

			stack = append(stack, deps...)

			for _, dep := range node.ForeignDepRefs {
				if selectedRef, ok := foreign[dep]; ok {
					dep = selectedRef
				}

				stack = append(stack, dep)
			}

			for _, resource := range node.Resources {
				if dep, ok := node.resourceRef(resource, ctx.emit.fetchRefs); ok {
					weak = append(weak, dep)
				}
			}
		}

		if !ctx.flushFbsContextBindings() && possible == len(bindings.order) {
			break
		}
	}

	weak = append(weak, ctx.emit.publicationRoots...)
	weak = append(weak, ctx.emit.results...)

	ctx.emit.exportEligible = ctx.activateSelectedInputs(activation, selected, weak)

	for _, node := range ctx.emit.nodes.s {
		if node == nil {
			continue
		}

		copied := false

		for i, dep := range node.ForeignDepRefs {
			tool, ok := bindings.byRef.get(dep)

			if !ok {
				continue
			}

			if tool.host == nil {
				if ctx.emit.exportEligible.has(uint32(node.Ref)) {
					ctx.onWarn(Warn{Kind: WarnToolConf, Message: fmt.Sprintf("%s: Tool is not found in a host graph", tool.path.string())})
				}

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
		Inputs:  NodeInputs{files: ctx.na.inputList(ctx.na.vfsList(from))},
		Outputs: ctx.na.vfsList(to),
		DepRefs: ctx.na.refList(tool.host.LDRef),
		KV:      &KV{P: pkCP, PC: pcLightBlue},
	})

	return tool.boundRef
}
