package main

var pchKV = KV{P: pkPC, PC: pcRed}

type PchFileStmt struct {
	Header ANY
	Output ANY
}

func (e *EmitContext) emitPchFile(stmt PchFileStmt) {
	na, d := e.ctx.na, e.d
	out := copyFileOutputVFS(e.instance.Path.relString(), stmt.Output.string())
	header := e.moduleSourceVFS(stmt.Header)
	toolRef, tool := e.ctx.tool(argSdgTier0ToolsMakePchWrapper)
	ref := e.ctx.emit.reserve()

	pending := e.pendingEmit(func() {
		closure := e.scanner.walkClosure(header, d.scanCtx, scanDomainCC)
		blocks := compilerCommandBlocks(&d.cc.CCBlocks, true)
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

		args := na.chunkList(na.anyList(tool.any(), llvmRoot, header.any(), out.any(), bldRootDirVFS.any(), srcRootDirVFS.any()),
			e.instance.Platform.CCHead, blocks.includes, blocks.flags[len(d.cc.ClangWarnings):], blocks.cxxTail, builtinMacroDateTime, macroPrefixMapFlags)

		inputs := na.inputList(na.vfsList(tool), na.dedupClosureChunks(closure)...)

		e.emitReservedNode(Node{Platform: e.instance.Platform, Cmds: na.cmdList(Cmd{CmdArgs: args, Env: envVarsVCS}),
			Env: envVarsVCS, Inputs: inputs, InputClosure: closure, Outputs: na.vfsList(out),
			DepRefs: na.refList(toolRef), Resources: resources, KV: &pchKV}, ref)
	})

	e.register(GeneratedFileInfo{OutputPath: out, PrimaryInput: header, ProducerRef: ref, GeneratorRefs: na.refList(toolRef), OnUse: pending})
}

func (e *EmitContext) pchLLVMRoot() ANY {
	name := strLlvmToolsRoot

	if value, ok := e.d.setVars[name]; ok {
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
