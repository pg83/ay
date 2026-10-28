package main

var fortranKV = KV{P: pkFL, PC: pcLightGreen}

func hasFortranGeneratedSource(d *ModuleData) bool {
	hasOutput := func(outputs []ANY, stdout *ANY, noAuto bool) bool {
		for _, out := range outputs {
			if srcExtClassOf(out) == srcExtFortran {
				return true
			}
		}

		return stdout != nil && !noAuto && srcExtClassOf(*stdout) == srcExtFortran
	}

	for _, stmt := range d.runPrograms {
		if hasOutput(stmt.OUTFiles, stmt.StdoutFile, stmt.StdoutNoAuto) {
			return true
		}
	}

	for _, stmt := range d.runPython {
		if hasOutput(stmt.OUTFiles, stmt.StdoutFile, stmt.StdoutNoAuto) {
			return true
		}
	}

	for _, stmt := range d.fromArchives {
		if hasOutput(stmt.OUTFiles, nil, true) {
			return true
		}
	}

	return false
}

func (e *EmitContext) prepareFortran() {
	cc := &e.d.cc

	if cc.FortranHead != nil {
		return
	}

	for _, resource := range e.peers.ResourceGlobals {
		if resource.GlobalVar.string() != "FLANG_RESOURCE_GLOBAL" {
			continue
		}

		flags := expandCommandTokens(e.ctx.na.anyList(strCFlagsPlatform.any()), e.instance.Platform.ifEnv)
		value := resourceGlobalArgumentValue(flags, e.peers.ResourceGlobals)

		flags = value.Args

		n := 1 + len(flags)

		cc.FortranHead = e.ctx.na.anys.alloc(n)[:n:n]
		cc.FortranHead[0] = internV(resource.Value.string(), "/bin/flang-new").any()
		copy(cc.FortranHead[1:], flags)
		e.ctx.na.anys.commit(len(cc.FortranHead))
		cc.FortranResources = e.ctx.na.strs.alloc(2)[:0]
		cc.FortranResources = append(cc.FortranResources, resource.Name)
		cc.FortranResourceUses = value.Resources

		if e.instance.Platform.UsesSDKRoot {
			cc.FortranResources = append(cc.FortranResources, internStr(resourcePatternOSSDKRoot))
		}

		e.ctx.na.strs.commit(len(cc.FortranResources))

		return
	}

	throwFmt("Fortran compiler resource missing from module peer closure")
}

func (e *EmitContext) emitFortranSource(meta SrcMeta) {
	e.prepareFortran()

	na := e.ctx.na
	src := e.sourceForEmission(meta)
	out, _ := composeCCPaths(e.instance, e.moduleSourceRel(meta.Source), src, ModuleCCInputs{FlatOutput: meta.Compile.FlatOutput}, ".o")
	closure := e.scanner.walkClosure(src, e.d.scanCtx, scanDomainCC)
	args := na.chunkList(e.d.cc.FortranHead, na.anyList(argDashC.any(), src.any(), argDashO.any(), out.any()))

	ref := e.emitNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: args, Env: envVarsVCS, Resources: e.d.cc.FortranResourceUses}), Env: envVarsVCS, Inputs: na.dedupClosureChunks(closure), Outputs: na.vfsList(out),
		KV: &fortranKV, Resources: e.d.cc.FortranResources}, input: InputSpec{root: closure.self},
	})

	e.collectObj(ref, out, meta)
}
