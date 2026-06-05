package main

const archiverToolPath = "tools/archiver"

func emitArchives(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.archives) == 0 {
		return
	}

	toolLDRef, toolBinPath := ctx.tool(archiverToolPath)

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

	cmdArgs := make([]string, 0, 4+len(a.Files)+2)
	cmdArgs = append(cmdArgs, toolBinPath.String(), "-q", "-x")

	if a.DontCompress {
		cmdArgs = append(cmdArgs, "-p")
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
		cmdArgs = append(cmdArgs, absStr+":")
	}

	cmdArgs = append(cmdArgs, "-o", archivePath)

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

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}
	tags := instance.Platform.Tags

	n := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: "AR", PC: "light-red"},
		Outputs:          []VFS{archiveVFS},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Tags:             tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		DepRefs:          depRefs,
	}

	arRef := emit.Emit(bindNodePlatform(withResources(n, resourcePatternYMakePython3, resourcePatternClangTool), instance.Platform))

	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    "AR",
			OutputPath:     archiveVFS,
			ProducerRef:    arRef,
			HasProducerRef: true,
		})
	}
}
