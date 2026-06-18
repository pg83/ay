package main

// emitBundles emits one BN (bundle) node per BUNDLE group and registers its
// build output so a later RESOURCE/embed of the bundled name resolves to the
// $(B) build artifact (via the codegen-registry probe in emitResourceObjcopy)
// instead of opening a nonexistent $(S)/<mod>/<name> source. Must run before
// the module's resource objcopy emit — same ordering FROM_SANDBOX relies on.
//
// Upstream's _BUNDLE_TARGET (build/ymake.core.conf): the BN node runs
// $MOVE_FILE (= fs_tools.py rename) over the bundled module's primary output
// (${result:Target}) into ${noauto;output:Destination} = $(B)/<mod>/<name>,
// and depends on the node producing that primary output.
func emitBundles(ctx *GenCtx, instance ModuleInstance, d *ModuleData) {
	reg := codegenRegForInstance(ctx, instance)

	for _, b := range d.bundles {
		dst := copyFileOutputVFS(instance.Path.rel(), b.Name)

		// A name already produced (e.g. a sibling COPY/BUNDLE) keeps its first
		// producer; register panics on a duplicate.
		if reg.lookup(dst) != nil {
			continue
		}

		src, srcRef, resolved := resolveBundleSource(ctx, instance, d, b.Target)

		ref := ctx.emit.reserve()
		emitBundleNode(ctx, instance, d.tc.Python3, src, dst, srcRef, resolved, ref)
		registerBoundGeneratedParsedOutput(ctx, instance, pkBN, dst, nil, ref, nil)
	}
}

// resolveBundleSource resolves the bundled module's primary build output and the
// node that produces it (${result:Target}). It peeks the bundled ya.make for a
// module opener first, so an unmodeled module type (e.g. PROTO_DESCRIPTIONS,
// which has no *ModuleStmt opener) never reaches genModule — which would throw
// on the unrecognized module macro. Returns resolved=false when the bundled
// target is absent, has no module opener, or exposes no linkable primary output.
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
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:              env,
		Inputs:           na.inputList(inputHead, ctx.scripts[fsTools]),
		KV:               KV{P: pkBN, PC: pcLightCyan},
		Outputs:          na.vfsList(dst),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		DepRefs:          depRefs,
		Resources:        usesPython3,
	}

	ctx.emit.emitReserved(node, id)
}
