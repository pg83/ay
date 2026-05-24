package main

// archiverToolPath is the upstream host-tool that the ARCHIVE() macro
// invokes per `build/ymake.core.conf:4142-4145` (`$ARCH_TOOL`).
const archiverToolPath = "tools/archiver"

// emitArchives emits one AR node per `ARCHIVE(NAME <out> [DONTCOMPRESS]
// files...)` declaration. Invokes the host archiver binary (resolved
// by walking tools/archiver as a host PROGRAM).
//
// cmd_args: `archiver -q -x [-p] <file1>: [<file2>:] -o <out>`. Each
// file gets a trailing colon (upstream `${suf=\:;input}`); PR-produced
// files resolve to BUILD_ROOT, others to $(S)/<modulePath>/<file>.
//
// Inputs: PR outputs ($(B)) + archiver tool + producer-PR IN files
// ($(S)). Deps: producer-PR NodeRef + archiver LDRef.
func emitArchives(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.archives) == 0 {
		return
	}

	// Walk the archiver as a host program to resolve its binary path + LD ref.
	toolLDRef, toolBinPath := ctx.tool(archiverToolPath)

	// Aggregate SOURCE_ROOT-rooted IN files contributed by every PR
	// in this module — REF includes the full upstream set in each
	// ARCHIVE's inputs[], not just the producing PR's IN list. Dedup once
	// in first-occurrence order (normalization sorts node inputs).
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

// emitArchive emits a single AR node for one ARCHIVE() declaration.
// Helper for emitArchives; split out so the tool-walk + shared input
// aggregation runs once per module rather than once per ARCHIVE.
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

	// Build cmd_args. Each archived file is rendered with a trailing
	// colon per upstream `${suf=\:;input:Files}`.
	cmdArgs := make([]string, 0, 4+len(a.Files)+2)
	cmdArgs = append(cmdArgs, toolBinPath.String(), "-q", "-x")
	if a.DontCompress {
		cmdArgs = append(cmdArgs, "-p")
	}

	// Track the unique producer PRs so we can wire deps to all of them
	// and add their BUILD_ROOT outputs to the AR's input set.
	producerRefs := []NodeRef{}
	producerSet := map[NodeRef]struct{}{}
	pathPerFile := make([]VFS, 0, len(a.Files))
	pathStrPerFile := make([]string, 0, len(a.Files))

	for _, f := range a.Files {
		// When the file matches a PR output of this module, resolve to
		// the producer's BUILD_ROOT-rooted path and record the PR
		// NodeRef for dep wiring. Otherwise treat as SOURCE_ROOT-
		// relative to the module dir.
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

	// REF's AR inputs include every upstream-PR output that is
	// lexicographically ≤ the archive's explicitly-referenced file.
	// ARCHIVE on `sitecustomize.pyc` lists sibling `__res.pyc`; the
	// inverse (ARCHIVE on `__res.pyc`) does NOT include the later
	// sibling. The lex gate keeps input ordering stable across
	// multiple archives sharing a producer.
	prSiblingOutputs := make([]VFS, 0)
	{
		// Largest archived file path (string form) becomes the upper
		// bound — siblings whose .String() form is strictly greater
		// are excluded. pathStrPerFile reuses the .String() materialised
		// while building cmd_args, so we don't re-allocate here.
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

	// inputs: each archived file's resolved path, then any sibling PR
	// outputs from the same producer (preserving REF's "all PR outputs
	// appear in every consumer's inputs" shape — sitecustomize.pyc.inc
	// lists __res.pyc even though it only archives sitecustomize.pyc),
	// then the tool binary, then the producer PR's source `IN` files
	// (rebased to SOURCE_ROOT, pre-aggregated by caller). Dedup
	// against the per-file slot.
	inputs := make([]VFS, 0, len(pathPerFile)+len(prSiblingOutputs)+1+len(prInSources))
	// BUILD_ROOT block = pathPerFile ∪ prSiblingOutputs, deduped in
	// first-occurrence order (pathPerFile then prSiblingOutputs).
	// Normalization sorts node inputs, so order here is irrelevant to the
	// gate; iterating the slices keeps it deterministic without a sort.
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

	// Empty `instance.Platform.Tags` keeps the slice non-nil so JSON
	// serialises as `[]`, not `null`.
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

	// Register the AR's output (.pyc.inc header for runtime_py3) in
	// the codegen registry. Consumer CCs (e.g. __res.cpp) carry the
	// .pyc.inc path via runtimePy3CCExtraInputs;
	// resolveCodegenDepRefs lifts ProducerRef into deps[].
	// EmitsIncludes is nil — .pyc.inc is a RESOURCE-packed C array,
	// not C-readable.
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    "AR",
			OutputPath:     archiveVFS,
			ProducerRef:    arRef,
			HasProducerRef: true,
		})
	}
}
