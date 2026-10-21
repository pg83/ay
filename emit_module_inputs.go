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

func (e *EmitContext) modulePeerInputs() InputChunks {
	if e.peers == nil {
		return nil
	}

	var inputs InputChunks

	for _, peer := range e.peers.Modules {
		if info := e.codegen.moduleInputs(peer); info != nil && len(info.ModuleIncludes) > 0 {
			inputs = append(inputs, info.ModuleIncludes)
		}
	}

	return inputs
}

func (e *EmitContext) appendModuleInputs(inputs InputChunks, useGlobals bool) InputChunks {
	peers := e.modulePeerInputs()
	count := len(inputs) + len(peers)

	if useGlobals && len(e.moduleGlobals) > 0 {
		count++
	}

	if count == len(inputs) {
		return inputs
	}

	out := e.ctx.na.inputs.alloc(count)
	k := copy(out, inputs)

	k += copy(out[k:], peers)

	if useGlobals && len(e.moduleGlobals) > 0 {
		out[k] = e.moduleGlobals
	}

	e.ctx.na.inputs.commit(count)

	return out[:count:count]
}

func (e *EmitContext) collectPeerModuleGlobals() []VFS {
	var peers [][]VFS

	if e.peers != nil {
		for _, peer := range e.peers.Modules {
			if info := e.codegen.moduleInputs(peer); info != nil && info.ModulePassPeers && len(info.ModuleGlobals) > 0 {
				peers = append(peers, info.ModuleGlobals)
			}
		}
	}

	if len(peers) == 0 {
		return e.ctx.na.dedupClosure(e.moduleGlobals)
	}

	if len(e.moduleGlobals) > 0 {
		peers = append(peers, e.moduleGlobals)
	}

	return e.ctx.na.dedupClosure(nil, peers)
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
		e.moduleGlobals = append(e.moduleGlobals, closure.self)

		for _, bucket := range closure.bucketList() {
			e.moduleGlobals = append(e.moduleGlobals, bucket...)
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

	passPeers := !isProgramModuleType(e.d.unit.Type) && !isDllModuleType(e.d.unit.Type)

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

	e.codegen.setModuleInputs(output, e.d.moduleStmt.Name, includes, globals, passPeers)
}
