package main

var bundleKV = KV{P: pkBN, PC: pcLightCyan}

func (e *EmitContext) emitBundles() {
	ctx, instance, d := e.ctx, e.instance, e.d
	reg := e.codegen

	for _, b := range d.bundles {
		dst := copyFileOutputVFS(instance.Path.relString(), b.Name)

		if reg.lookup(dst) != nil {
			continue
		}

		src, srcRef, resolved := e.resolveBundleSource(b.Target)
		ref := ctx.emit.reserve()

		emitBundleNode(ctx, instance, d.tc.Python3, src, dst, srcRef, resolved, ref)

		reg.register(&GeneratedFileInfo{
			OutputPath:    dst,
			ProducerRef:   ref,
			GeneratorRefs: nil,
		})
	}
}

func (e *EmitContext) resolveBundleSource(target string) (VFS, NodeRef, bool) {
	ctx := e.ctx

	if !peerYaMakeExists(ctx.fs, dirKey(target).source()) {
		return 0, 0, false
	}

	if !hasModuleOpener(moduleStmts(ctx, target)) {
		return 0, 0, false
	}

	res := genModule(ctx, e.derivePeerInstance(target))

	if res.isPROGRAM && res.LDPath != nil {
		return *res.LDPath, res.LDRef, true
	}

	if res.ARPath != nil {
		return *res.ARPath, res.ARRef, true
	}

	return 0, 0, false
}

func hasModuleOpener(stmts []Stmt) bool {
	for _, s := range stmts {
		if _, ok := s.(*ModuleStmt); ok {
			return true
		}
	}

	return false
}

func emitBundleNode(ctx *GenCtx, instance ModuleInstance, python3 VFS, src, dst VFS, srcRef NodeRef, resolved bool, id NodeRef) {
	na := ctx.emit.nodeArenas()
	fsTools := copyFsToolsVFS
	cmdArgs := make([]ANY, 0, 5)

	cmdArgs = append(cmdArgs, python3.any(), fsTools.any(), argRename.any())

	if resolved {
		cmdArgs = append(cmdArgs, src.any())
	}

	cmdArgs = append(cmdArgs, dst.any())

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}

	var inputHead []VFS
	var depRefs []NodeRef

	if resolved {
		inputHead = []VFS{src}
		depRefs = []NodeRef{srcRef}
	}

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputHead, ctx.scripts[fsTools.rel()]),
		KV:           &bundleKV,
		Outputs:      na.vfsList(dst),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      depRefs,
		Resources:    usesPython3,
	}

	ctx.emit.emitReservedNode(node, id)
}
