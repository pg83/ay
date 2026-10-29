package main

var pchKV = KV{P: pkPC, PC: pcRed}

type PchFileStmt struct {
	Header ANY
	Output ANY
}

type pchCommand struct {
	emit   *EmitContext
	header VFS
	out    VFS
	tool   VFS
	ref    NodeRef
	scoped ScopedCommand
}

func (e *EmitContext) emitPchFile(stmt PchFileStmt) {
	na := e.ctx.na
	out := copyFileOutputVFS(e.instance.Path.relString(), stmt.Output.string())
	header := e.moduleSourceVFS(stmt.Header)
	toolRef, tool := e.ctx.tool(argSdgTier0ToolsMakePchWrapper)
	ref := e.ctx.emit.reserve()
	pch := &pchCommand{emit: e, header: header, out: out, tool: tool, ref: ref}

	e.frame.pending++

	pending := e.pendingEmit(func() {
		llvmRoot := e.pchLLVMRoot()
		resources := na.strs.alloc(3)[:0]

		resources = append(resources, strYMakePython3Name)

		for _, resource := range e.peers.ResourceGlobals {
			if resource.Value.string() == llvmRoot.string() {
				resources = append(resources, resource.Name)

				break
			}
		}

		if e.instance.Platform.UsesSDKRoot {
			resources = append(resources, internStr(resourcePatternOSSDKRoot))
		}

		na.strs.commit(len(resources))

		inputs := na.inputList(na.vfsList(tool), na.vfsList(header))

		e.emitReservedNode(NodeSpec{Node: Node{Platform: e.instance.Platform, Cmds: pch.cmds(e.ctx.visitScoped(&pch.scoped)),
			Env: envVarsVCS, Inputs: inputs, Outputs: na.vfsList(out),
			DepRefs: na.refList(toolRef), Resources: resources, KV: &pchKV}, input: InputSpec{roots: inputs[1:]},
		}, ref)

		pch.scoped.rendered = pch
	})

	e.register(GeneratedFileSpec{OutputPath: out, PrimaryInput: header, ProducerRef: ref, GeneratorRefs: na.refList(toolRef), OnUse: pending, Scoped: &pch.scoped})
}

func (p *pchCommand) cmds(scope *CommandScope) []Cmd {
	e := p.emit
	na, d := e.ctx.na, e.d
	blocks := compilerCommandBlocks(e.pchCCBlocks(scope), true)

	args := na.chunkList(na.anyList(p.tool.any(), e.pchLLVMRoot(), p.header.any(), p.out.any(), bldRootDirVFS.any(), srcRootDirVFS.any()),
		e.instance.Platform.CCHead, blocks.includes, blocks.flags[len(d.cc.ClangWarnings):], blocks.cxxTail, builtinMacroDateTime, macroPrefixMapFlags)

	return na.cmdList(Cmd{Resources: blocks.resources, CmdArgs: args, Env: envVarsVCS})
}

func (p *pchCommand) rebindScope(scope *CommandScope) {
	p.emit.ctx.emit.nodes.s[p.ref].Cmds = p.cmds(scope)
}

func (e *EmitContext) pchCCBlocks(scope *CommandScope) *CcModuleArgBlocks {
	if scope == nil || scope == e.scope {
		return &e.d.cc.CCBlocks
	}

	env := e.d.cc

	env.AddIncl, env.PeerAddInclGlobal = scope.addIncl, scope.peerAddInclGlobal
	env.PeerCFlagsGlobal, env.OwnCFlagsGlobal = scope.peerCFlagsGlobal, scope.ownCFlagsGlobal
	env.PeerCXXFlagsGlobal, env.OwnCXXFlagsGlobal = scope.peerCXXFlagsGlobal, scope.ownCXXFlagsGlobal

	blocks := composeCCModuleArgBlocks(e.ctx.na, e.instance.Platform, &env)

	return &blocks
}

func (e *EmitContext) pchLLVMRoot() ANY {
	name := strLlvmToolsRoot

	if value, ok := e.d.setVars.get(name), e.d.setVars.has(name); ok {
		return internStr(resolveResourceGlobalRef(value.string(), e.peers.ResourceGlobals)).any()
	}

	if value, ok := e.instance.Platform.Flags[envLlvmToolsRoot]; ok {
		return internStr(resolveResourceGlobalRef(value.string(), e.peers.ResourceGlobals)).any()
	}

	for _, resource := range e.peers.ResourceGlobals {
		if resource.GlobalVar == strClang20ResourceGlobal {
			return resource.Value.any()
		}
	}

	return strEmpty.any()
}
