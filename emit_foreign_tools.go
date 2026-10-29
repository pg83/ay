package main

import (
	"fmt"
	"slices"
)

type ForeignTool struct {
	path     ARG
	host     *ModuleEmitResult
	boundRef NodeRef
}

func (e *EmitContext) foreignToolResult(path ARG) *ModuleEmitResult {
	ctx := e.ctx

	if e.instance.Platform == ctx.host || e.instance.Platform == ctx.host.picVariant || isJavaModuleType(ctx.moduleName(path.string())) {
		return ctx.toolResult(path)
	}

	instance := ctx.rootInstance(path.string(), demandLinked)

	instance.Platform = e.instance.Platform

	if result := ctx.memoGet(instance); result != nil && result.ForeignTool != nil {
		ctx.consumeModuleWarnings(result)

		return result
	}

	result := func() *ModuleEmitResult {
		ctx.emit.deferPublication++

		defer func() { ctx.emit.deferPublication-- }()

		return genModule(ctx, instance)
	}()

	ctx.consumeModuleWarnings(result)

	ctx.moduleByRef.put(result.LDRef, result)

	result.ForeignTool = &ForeignTool{path: path}

	return result
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

func (ctx *GenCtx) activateNodeInputs(node *Node, selectProducer func(NodeRef), modulePeers bool) {
	if len(node.InputFixes) == 0 {
		return
	}

	f := ctx.scannerTarget.fixer

	var added []NodeRef
	var registry *CodegenRegistry
	var buildDeps []NodeRef

	module := false

	for _, id := range node.InputFixes {
		entry := f.entries.s[id-1]
		peer := entry.kind == fixPeerIncludes || entry.kind == fixModuleRoots

		if peer != modulePeers {
			continue
		}

		registry = entry.scanner.codegen
		buildDeps, module = registry.moduleBuildDeps.get(node.Ref)
		module = module && !modulePeers

		f.activate(id, func(ref NodeRef) {
			selectProducer(ref)

			if ref == node.Ref || slices.Contains(node.DepRefs, ref) && (!module || slices.Contains(buildDeps, ref)) {
				return
			}

			if added == nil {
				added = nodeRefScratches.get()
			}

			added = append(added, ref)
		})
	}

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

func (ctx *GenCtx) hasPeerInputFixes(node *Node) bool {
	for _, id := range node.InputFixes {
		kind := ctx.scannerTarget.fixer.entries.s[id-1].kind

		if kind == fixPeerIncludes || kind == fixModuleRoots {
			return true
		}
	}

	return false
}

func (ctx *GenCtx) foreignToolOf(ref NodeRef) *ModuleEmitResult {
	if result, ok := ctx.moduleByRef.get(ref); ok && result.ForeignTool != nil {
		return result
	}

	return nil
}

func (ctx *GenCtx) requireHostTool(path ARG) *ModuleEmitResult {
	host := ctx.toolResult(path)

	host.ToolRequired = true
	ctx.scannerHost.codegen.use(*host.LDPath)

	return host
}

func (ctx *GenCtx) bindForeignTools() {
	weak := nodeRefScratches.get()

	defer func() { nodeRefScratches.put(weak) }()

	var selected BitSet
	var deferred []NodeRef

	stack := append([]NodeRef(nil), ctx.emit.results...)

	selectProducer := func(ref NodeRef) {
		stack = append(stack, ref)
	}

	for {
		for len(stack) > 0 {
			ref := stack[len(stack)-1]

			stack = stack[:len(stack)-1]

			if selected.has(uint32(ref)) {
				continue
			}

			node := ctx.emit.nodes.s[ref]

			if node == nil {
				deferred = append(deferred, ref)

				continue
			}

			selected.add(uint32(ref))
			ctx.activateNodeInputs(node, selectProducer, false)

			if ctx.hasPeerInputFixes(node) {
				weak = append(weak, ref)
			}

			var foreign map[NodeRef]NodeRef

			for _, dep := range node.ForeignDepRefs {
				result, ok := ctx.moduleByRef.get(dep)

				if !ok {
					continue
				}

				if tool := result.ForeignTool; tool != nil {
					if tool.host == nil {
						tool.host = ctx.toolResult(tool.path)
					}

					ctx.requireHostTool(tool.path)

					if foreign == nil {
						foreign = make(map[NodeRef]NodeRef)
					}

					foreign[dep] = tool.host.LDRef
				} else if result.ToolPath != 0 {
					ctx.requireHostTool(result.ToolPath)
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

		emitted := ctx.flushFbsFlatcRecipes()
		rest := deferred[:0]

		for _, ref := range deferred {
			if ctx.emit.nodes.s[ref] != nil {
				stack = append(stack, ref)
			} else {
				rest = append(rest, ref)
			}
		}

		deferred = rest

		if len(stack) == 0 && !emitted {
			break
		}
	}

	weak = append(weak, ctx.emit.publicationRoots...)
	weak = append(weak, ctx.emit.results...)

	ctx.emit.exportEligible = ctx.activateSelectedInputs(selected, weak)

	for _, node := range ctx.emit.nodes.s {
		if node == nil {
			continue
		}

		copied := false

		for i, dep := range node.ForeignDepRefs {
			target := ctx.foreignToolOf(dep)

			if target == nil {
				continue
			}

			tool := target.ForeignTool
			host, _ := ctx.tools.get(tool.path)

			if host == nil || !host.ToolRequired {
				if ctx.emit.exportEligible.has(uint32(node.Ref)) {
					ctx.onWarn(Warn{Kind: WarnToolConf, Message: fmt.Sprintf("%s: Tool is not found in a host graph", tool.path.string())})
				}

				continue
			}

			tool.host = host

			if !copied {
				node.ForeignDepRefs = ctx.na.refList(node.ForeignDepRefs...)
				copied = true
			}

			node.ForeignDepRefs[i] = ctx.foreignToolRef(target)
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

func (ctx *GenCtx) foreignToolRef(target *ModuleEmitResult) NodeRef {
	tool := target.ForeignTool

	if tool.boundRef != 0 {
		return tool.boundRef
	}

	if *target.LDPath == *tool.host.LDPath {
		return tool.host.LDRef
	}

	from, to := *tool.host.LDPath, *target.LDPath

	tool.boundRef = ctx.emit.emitNode(NodeSpec{Node: Node{Platform: ctx.host,
		Cmds: ctx.na.cmdList(Cmd{
			CmdArgs: ctx.na.chunkList(ctx.na.anyList(strBinMv.any(), from.any(), to.any())),
			Cwd:     bldRootDirVFS,
		}), Inputs: ctx.na.inputList(ctx.na.vfsList(from)), Outputs: ctx.na.vfsList(to),
		DepRefs: ctx.na.refList(tool.host.LDRef),
		KV:      &KV{P: pkCP, PC: pcLightBlue}},
	})

	return tool.boundRef
}
