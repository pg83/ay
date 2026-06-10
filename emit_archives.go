package main

func emitArchives(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.archives) == 0 {
		return
	}

	toolLDRef, toolBinPath := ctx.tool(argToolsArchiver)

	reg := codegenRegForInstance(ctx, instance)

	for _, a := range d.archives {
		emitArchive(instance, a, d, toolBinPath, toolLDRef, ctx.emit, reg)
	}
}

func emitArchive(
	instance ModuleInstance,
	a archiveEntry,
	d *moduleData,
	toolBinPath VFS,
	toolLDRef NodeRef,
	emit Emitter,
	reg *CodegenRegistry,
) {
	archiveVFS := Build(instance.Path + "/" + a.Name)
	archivePath := archiveVFS.String()

	cmdArgs := make([]STR, 0, 4+len(a.Files)+2)
	cmdArgs = append(cmdArgs, (toolBinPath).str(), argQ.str(), argX.str())

	if a.DontCompress {
		cmdArgs = append(cmdArgs, argP.str())
	}

	producerRefs := []NodeRef{}
	producerSet := map[NodeRef]struct{}{}
	pathPerFile := make([]VFS, 0, len(a.Files))

	for _, f := range a.Files {
		isPRProduced := false

		if d.prOutputProducer != nil {
			if ref, ok := d.prOutputProducer[f]; ok {
				isPRProduced = true

				if _, dup := producerSet[ref]; !dup {
					producerSet[ref] = struct{}{}
					producerRefs = append(producerRefs, ref)
				}
			}
		}

		rel := instance.Path + "/" + f
		var absVFS VFS

		if isPRProduced {
			absVFS = Build(rel)
		} else {
			absVFS = Source(rel)
		}

		absStr := absVFS.String()

		pathPerFile = append(pathPerFile, absVFS)
		cmdArgs = append(cmdArgs, internStr(absStr+":"))
	}

	cmdArgs = append(cmdArgs, argDashO.str(), internStr(archivePath))

	// Archive-node inputs are exactly the files the archiver reads (the archived
	// members) plus the archiver tool. RUN_PROGRAM source INFiles and non-archived
	// sibling PR outputs the command never names — they are build-order concerns
	// carried by producerRefs / toolLDRef DepRefs, not action inputs — so they are
	// not listed here.
	inputs := make([]VFS, 0, len(pathPerFile)+1)
	buildRootSeen := map[VFS]struct{}{}

	for _, p := range pathPerFile {
		if _, dup := buildRootSeen[p]; dup {
			continue
		}

		buildRootSeen[p] = struct{}{}
		inputs = append(inputs, p)
	}

	inputs = append(inputs, toolBinPath)

	depRefs := make([]NodeRef, 0, len(producerRefs)+1)
	depRefs = append(depRefs, producerRefs...)

	if toolLDRef != (NodeRef(0)) {
		depRefs = append(depRefs, toolLDRef)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	n := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkAR, PC: pcLightRed},
		Outputs:          []VFS{archiveVFS},
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		DepRefs:          depRefs,
		usesResources:    []string{resourcePatternYMakePython3, resourcePatternClangTool + instance.Platform.ClangVer},
	}

	arRef := emit.Emit(n)

	if reg != nil {
		// Propagate each archived member's source inputs (e.g. the .py behind a
		// .pyc compiled by a RUN_PROGRAM) as non-expanded closure leaves of the
		// archive output, so a CC unit that #includes the archived .inc picks them
		// up transitively through the cached window — replacing the former
		// per-CC-source fixup for the runtime_py3 bootstrap.
		var leaves []VFS

		for _, p := range pathPerFile {
			if info := reg.Lookup(p); info != nil && len(info.SourceInputs) > 0 {
				leaves = dedupVFS(leaves, info.SourceInputs)
			}
		}

		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    pkAR,
			OutputPath:     archiveVFS,
			ProducerRef:    arRef,
			HasProducerRef: true,
			ClosureLeaves:  leaves,
		})
	}
}
