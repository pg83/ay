package main

const archiverToolPath = "tools/archiver"

func emitArchives(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.archives) == 0 {
		return
	}

	toolLDRef, toolBinPath := ctx.tool(archiverToolPath)

	var prInSources []VFS
	{
		seen := map[VFS]struct{}{}
		for _, rp := range d.runPrograms {
			for _, f := range rp.INFiles {
				p := Source(instance.Path + "/" + f)
				if _, dup := seen[p]; dup {
					continue
				}
				seen[p] = struct{}{}
				prInSources = append(prInSources, p)
			}
		}
	}

	reg := codegenRegForInstance(ctx, instance)
	for _, a := range d.archives {
		emitArchive(instance, a, d, toolBinPath, toolLDRef, prInSources, ctx.emit, reg)
	}
}

func emitArchive(
	instance ModuleInstance,
	a archiveEntry,
	d *moduleData,
	toolBinPath VFS,
	toolLDRef NodeRef,
	prInSources []VFS,
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
	pathStrPerFile := make([]string, 0, len(a.Files))

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
		pathStrPerFile = append(pathStrPerFile, absStr)
		cmdArgs = append(cmdArgs, absStr+":")
	}
	cmdArgs = append(cmdArgs, "-o", archivePath)

	prSiblingOutputs := make([]VFS, 0)
	{

		maxArchived := ""
		for _, s := range pathStrPerFile {
			if s > maxArchived {
				maxArchived = s
			}
		}
		seen := map[VFS]struct{}{}
		for _, p := range pathPerFile {
			seen[p] = struct{}{}
		}
		for _, rp := range d.runPrograms {
			rpProduces := false
			for _, f := range rp.OUTFiles {
				if _, ok := producerSet[d.prOutputProducer[f]]; ok {
					rpProduces = true
					break
				}
			}
			if !rpProduces {
				for _, f := range rp.OUTNoAutoFiles {
					if _, ok := producerSet[d.prOutputProducer[f]]; ok {
						rpProduces = true
						break
					}
				}
			}
			if !rpProduces && rp.StdoutFile != nil {
				if _, ok := producerSet[d.prOutputProducer[*rp.StdoutFile]]; ok {
					rpProduces = true
				}
			}
			if !rpProduces {
				continue
			}
			collect := func(rel string) {
				v := Build(instance.Path + "/" + rel)
				if v.String() > maxArchived {
					return
				}
				if _, dup := seen[v]; dup {
					return
				}
				seen[v] = struct{}{}
				prSiblingOutputs = append(prSiblingOutputs, v)
			}
			for _, f := range rp.OUTFiles {
				collect(f)
			}
			for _, f := range rp.OUTNoAutoFiles {
				collect(f)
			}
			if rp.StdoutFile != nil {
				collect(*rp.StdoutFile)
			}
		}
	}

	inputs := make([]VFS, 0, len(pathPerFile)+len(prSiblingOutputs)+1+len(prInSources))

	buildRootSeen := map[VFS]struct{}{}
	for _, p := range pathPerFile {
		if _, dup := buildRootSeen[p]; dup {
			continue
		}
		buildRootSeen[p] = struct{}{}
		inputs = append(inputs, p)
	}
	for _, p := range prSiblingOutputs {
		if _, dup := buildRootSeen[p]; dup {
			continue
		}
		buildRootSeen[p] = struct{}{}
		inputs = append(inputs, p)
	}
	inputs = append(inputs, toolBinPath)
	inSet := map[VFS]struct{}{}
	for _, p := range inputs {
		inSet[p] = struct{}{}
	}
	for _, p := range prInSources {
		if _, dup := inSet[p]; dup {
			continue
		}
		inSet[p] = struct{}{}
		inputs = append(inputs, p)
	}

	depRefs := make([]NodeRef, 0, len(producerRefs)+1)
	depRefs = append(depRefs, producerRefs...)
	if toolLDRef != (NodeRef{}) {
		depRefs = append(depRefs, toolLDRef)
	}

	env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}

	tags := instance.Platform.Tags

	n := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]interface{}{
			"p":  "AR",
			"pc": "light-red",
		},
		Outputs:  []VFS{archiveVFS},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		DepRefs: depRefs,
	}

	arRef := emit.Emit(bindNodePlatform(n, instance.Platform))

	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    "AR",
			OutputPath:     archiveVFS,
			ProducerRef:    arRef,
			HasProducerRef: true,
		})
	}
}
