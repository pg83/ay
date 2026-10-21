package main

import "path"

func moduleOutputPath(result *ModuleEmitResult) VFS {
	if result.ArchiveFile != 0 {
		return result.ArchiveFile
	}

	if result.ARPath != nil {
		return *result.ARPath
	}

	if result.LDPath != nil {
		return *result.LDPath
	}

	return 0
}

func (e *EmitContext) appendModulePeer(paths []VFS, result *ModuleEmitResult, capacity int) []VFS {
	output := moduleOutputPath(result)

	if output == 0 {
		return paths
	}

	info := e.codegen.moduleInputs(output)

	if info == nil || len(info.Includes) == 0 && len(info.Globals) == 0 {
		return paths
	}

	if paths == nil {
		paths = e.ctx.na.vfs.alloc(capacity)[:0]
	}

	return append(paths, output)
}

func (e *EmitContext) modulePeerInputs() InputChunks {
	if e.peers == nil {
		return nil
	}

	var inputs InputChunks

	for _, peer := range e.peers.Modules {
		if info := e.codegen.moduleInputs(peer); info != nil && len(info.Includes) > 0 {
			inputs = append(inputs, info.Includes)
		}
	}

	return inputs
}

func (e *EmitContext) appendModuleInputs(inputs InputChunks, useGlobals bool) InputChunks {
	peers := e.modulePeerInputs()
	count := len(inputs) + len(peers)

	if useGlobals && len(e.moduleGlobalInputs) > 0 {
		count++
	}

	if count == len(inputs) {
		return inputs
	}

	var result InputChunks

	dedupers.with(func(dd *DeDuper) {
		out := e.ctx.na.inputs.alloc(count)
		k := copy(out, inputs)

		for _, chunk := range inputs {
			for _, input := range chunk {
				dd.addStable(input.strID())
			}
		}

		for _, chunk := range peers {
			if unseen := e.ctx.na.filterSeen(dd, chunk); len(unseen) > 0 {
				out[k] = unseen
				k++
			}
		}

		if useGlobals {
			if unseen := e.ctx.na.filterSeen(dd, e.moduleGlobalInputs); len(unseen) > 0 {
				out[k] = unseen
				k++
			}
		}

		e.ctx.na.inputs.commit(k)
		result = out[:k:k]
	})

	return result
}

func (e *EmitContext) collectPeerModuleGlobals() {
	var peers, inputs [][]VFS

	if e.peers != nil {
		for _, peer := range e.peers.Modules {
			if info := e.codegen.moduleInputs(peer); info != nil && info.PassPeers && len(info.Globals) > 0 {
				peers = append(peers, info.Globals)
				inputs = append(inputs, info.GlobalInputs)
			}
		}
	}

	if len(peers) == 0 {
		e.moduleGlobals = e.ctx.na.dedupClosure(e.moduleGlobals)
		e.moduleGlobalInputs = e.ctx.na.dedupClosure(e.moduleGlobalInputs)

		return
	}

	if len(e.moduleGlobals) > 0 {
		peers = append(peers, e.moduleGlobals)
		inputs = append(inputs, e.moduleGlobalInputs)
	}

	e.moduleGlobals = e.ctx.na.dedupClosure(nil, peers)
	e.moduleGlobalInputs = e.ctx.na.dedupClosure(nil, inputs)
}

func (e *EmitContext) collectModuleGlobalSource(src ANY) bool {
	switch path.Ext(src.string()) {
	case ".a", ".lib", ".mf", ".supp", ".sbom", ".ld", ".sdcmeta", ".pyplugin":
	default:
		return false
	}

	input := e.moduleSourceVFS(src)

	if input.isSource() {
		sc := e.scanner.getScanCtx(e.d.scanCtx, scanDomainCC, EmptyIncludeDirectiveParser{})
		closure := sc.closureOf(input)

		e.scanner.putScanCtx(sc)
		e.moduleGlobals = append(e.moduleGlobals, input)
		e.moduleGlobalInputs = append(e.moduleGlobalInputs, closure.self)

		for _, bucket := range closure.bucketList() {
			e.moduleGlobalInputs = append(e.moduleGlobalInputs, bucket...)
		}
	}

	return true
}

func (e *EmitContext) publishModuleIncludes(result *ModuleEmitResult) {
	output := moduleOutputPath(result)

	if output == 0 {
		return
	}

	var peerInputs InputChunks

	passPeers := !isProgramModuleType(e.d.unit.Type) && !isDllModuleType(e.d.unit.Type) && e.d.unit.Type != tokDynamicLibrary

	if value, ok := e.d.setVars[strPassPeers]; ok {
		passPeers = stringIsTruthy(value.string())
	}

	if passPeers {
		peerInputs = e.modulePeerInputs()
	}

	globals := e.moduleGlobals

	if len(e.moduleIncludes) == 0 && len(peerInputs) == 0 && len(globals) == 0 {
		return
	}

	includes := e.ctx.na.dedupClosure(e.moduleIncludes, peerInputs)

	e.codegen.setModuleInputs(output, e.d.moduleStmt.Name, includes, globals, e.moduleGlobalInputs, passPeers)
}

func (e *EmitContext) globalPlugins() []VFS {
	var plugins []VFS

	for _, input := range e.moduleGlobals {
		if path.Ext(input.relString()) == ".pyplugin" {
			plugins = append(plugins, input)
		}
	}

	return plugins
}

func (e *EmitContext) emitLDPlugins() {
	for _, name := range e.d.ldPlugins {
		src := e.moduleSourceVFS(name)
		dst := build(e.instance.Path.relString(), "/", name.string(), ".pyplugin")
		ref := e.ctx.emit.reserve()
		instance := e.instance

		instance.Platform = e.ctx.target

		node := composeCPNode(instance, src, dst, nil, e.d.tc, e.ctx.scripts, e.ctx.na)

		e.ctx.emit.emitReservedNode(node, ref)

		e.register(GeneratedFileInfo{OutputPath: dst, PrimaryInput: src, ProducerRef: ref})
		e.moduleGlobals = append(e.moduleGlobals, dst)
		e.moduleGlobalInputs = append(e.moduleGlobalInputs, dst)
		e.moduleIncludes = append(e.moduleIncludes, dst)
	}
}
