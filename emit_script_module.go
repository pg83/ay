package main

import "strings"

func (e *EmitContext) pythonScriptCmd(script string, args ...string) Cmd {
	na := e.ctx.na
	head := []ANY{e.d.tc.Python3, source("build/scripts/", script).any()}

	for _, arg := range args {
		head = append(head, internStr(arg).any())
	}

	return Cmd{CmdArgs: na.chunkList(na.anyList(head...)), Env: envVarsVCS}
}

func scriptInputs(scripts ...string) []VFS {
	inputs := make([]VFS, 0, len(scripts))

	for _, script := range scripts {
		inputs = append(inputs, source("build/scripts/", script))
	}

	return inputs
}

func (e *EmitContext) emitLazyModuleNode(node NodeSpec, needs []VFS, ownedOutputs ...VFS) NodeRef {
	ctx, reg, na := e.ctx, e.codegen, e.ctx.na
	ref := ctx.emit.reserve()

	needs = append([]VFS{}, needs...)

	for _, chunk := range node.Inputs {
		for _, input := range chunk {
			if input.isBuild() {
				needs = append(needs, input)
			}
		}
	}

	needs = dedup(needs)

	refs := append([]NodeRef{}, node.DepRefs...)

	for _, chunk := range node.Inputs {
		for _, input := range chunk {
			if info := reg.lookup(input); info != nil {
				refs = append(refs, info.ProducerRef)
			}
		}
	}

	node.DepRefs = na.refList(dedupRefs(refs)...)

	pending := e.pendingEmit(func() {
		if node.Module {
			previous := ctx.pushFrame()

			ctx.frames[ctx.frameDepth-1] = e.frame

			defer func() {
				ctx.frames[ctx.frameDepth-1] = previous
				ctx.popFrame()
			}()
		}

		for _, input := range needs {
			reg.use(input)
		}

		e.emitReservedNode(node, ref)
	})

	if len(ownedOutputs) == 0 {
		ownedOutputs = node.Outputs
	}

	var templates []VFS

	if node.input.roots == nil {
		node.input.roots = node.Inputs
	}

	for _, chunk := range node.input.roots {
		for _, input := range chunk {
			if input.isBuild() && strings.HasSuffix(input.relString(), ".in") && reg.lookup(input) != nil {
				templates = append(templates, input)
			}
		}
	}

	templates = na.dedupClosure(templates)

	var includedInputs *IncludedInputPlan

	if len(templates) > 0 {
		includedInputs = &IncludedInputPlan{Roots: templates}
	}

	for _, out := range ownedOutputs {
		reg.register(GeneratedFileSpec{OutputPath: out, OwnerModule: e.instance.Path, OwnerSerial: e.frame.serial, ProducerRef: ref, OnUse: pending, IncludedBuildInputs: templates, IncludedInputs: includedInputs})
	}

	return ref
}

func (e *EmitContext) scriptModuleResources(globals []ResourceDecl, peers ...string) []ResourceDecl {
	for _, peer := range peers {
		if !peerYaMakeExists(e.ctx.fs, source(peer)) {
			continue
		}

		res := e.genPeerModule(ModuleInstance{Path: source(peer), Kind: KindLib, Platform: e.instance.Platform})

		globals = append(globals, res.ResourceGlobalClosure...)
	}

	e.frame.peerCtx = PeerContext{ResourceGlobals: globals}
	e.peers = &e.frame.peerCtx
	e.d.tc = resolveModuleToolchain(e.ctx, globals, e.instance.Platform)

	if e.d.tc.Python3 == 0 {
		e.d.tc.Python3 = build("resources/", resourcePatternYMakePython3, "/bin/python3").any()
	}

	e.d.cc.TC = e.d.tc

	e.d.scanCtx = newModuleScanContext(e.ctx, e.instance, e.d,
		e.ctx.na.vfsList(dedup(e.d.addIncl, e.d.addInclGlobal)...), nil, nil,
		e.ctx.na.vfsList(dedup(e.d.protoAddInclGlobal, e.d.protoAddIncl)...))

	return globals
}

func (e *EmitContext) scriptModuleResult(ref NodeRef, out VFS, peers []VFS, globals []ResourceDecl) *ModuleEmitResult {
	res := &ModuleEmitResult{ARRef: ref, ARPath: ptr(out), LDRef: ref, LDPath: ptr(out), ModuleStmtName: e.d.moduleStmt.Name, ResourceGlobalClosure: globals}

	res.JavaClosure = append(append([]VFS{}, peers...), out)

	languages := []Language{e.instance.Language, LangNone, LangCPP}

	if e.d.multimodule != 0 && e.instance.Language != LangCPP {
		languages = languages[:1]
	}

	for _, kind := range []ModuleKind{KindLib, KindBin} {
		for _, language := range languages {
			for _, demand := range []ModuleDemand{demandNone, demandSelf, demandLinked} {
				alias := e.instance

				alias.Kind, alias.Language, alias.Demand = kind, language, demand
				e.ctx.memoPut(alias, res)
			}
		}
	}

	return res
}
