package main

func emitBundles(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	reg := ctx.codegenFor(instance)

	for _, b := range d.bundles {
		dst := copyFileOutputVFS(instance.Path.rel(), b.Name)

		if reg.lookup(dst) != nil {
			continue
		}

		src, srcRef, resolved := resolveBundleSource(ctx, instance, d, b.Target)
		ref := ctx.emit.reserve()

		emitBundleNode(ctx, instance, d.tc.Python3, src, dst, srcRef, resolved, ref)
		reg.register(&GeneratedFileInfo{
			ProducerKvP:    pkBN,
			OutputPath:     dst,
			ProducerRef:    ref,
			GeneratorRefs:  nil,
			ParsedIncludes: nil,
		})
	}
}

func resolveBundleSource(ctx *GenCtx, parent ModuleInstance, d *ModuleData, target string) (VFS, NodeRef, bool) {
	if !peerYaMakeExists(ctx.fs, target) {
		return 0, 0, false
	}

	if !hasModuleOpener(moduleStmts(ctx, target)) {
		return 0, 0, false
	}

	res := genModule(ctx, derivePeerInstance(ctx, parent, d, target))

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

func emitBundleNode(ctx *GenCtx, instance ModuleInstance, python3 STR, src, dst VFS, srcRef NodeRef, resolved bool, id NodeRef) {
	na := ctx.emit.nodeArenas()
	fsTools := copyFsToolsVFS
	cmdArgs := make([]STR, 0, 5)

	cmdArgs = append(cmdArgs, python3, fsTools.str(), argRename.str())

	if resolved {
		cmdArgs = append(cmdArgs, src.str())
	}

	cmdArgs = append(cmdArgs, dst.str())

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	var inputHead []VFS
	var depRefs []NodeRef

	if resolved {
		inputHead = []VFS{src}
		depRefs = []NodeRef{srcRef}
	}

	node := &Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputHead, ctx.scripts[fsTools]),
		KV:           &bundleKV,
		Outputs:      na.vfsList(dst),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      depRefs,
		Resources:    usesPython3,
	}

	ctx.emit.emitReserved(node, id)
}

var (
	bundleKV = KV{P: pkBN, PC: pcLightCyan}
)
