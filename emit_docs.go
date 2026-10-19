package main

import "strings"

var (
	docsKV           = KV{P: pkDO, PC: pcLightCyan, ShowOut: true}
	docsRequirements = Requirements{CPU: 4, Network: nwRestricted, RAM: 32}
)

func (e *EmitContext) genDocs() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	docs := d.docs
	module := instance.Path.relString()

	var peers []VFS
	var globals []ResourceDecl

	for _, p := range d.peerdirs {
		peer := p.string()
		name := ctx.moduleName(peer)

		if name != tokDocs && name != tokDocsLibrary {
			continue
		}

		res := genModule(ctx, ModuleInstance{Path: source(peer), Kind: KindLib, Language: instance.Language, Platform: instance.Platform})

		peers = append(peers, res.DocsClosure...)
		globals = append(globals, res.ResourceGlobalClosure...)
	}

	resourcePeers := []string{"build/platform/python/ymake_python3"}

	if d.moduleStmt.Name == tokDocs {
		resourcePeers = append(resourcePeers, "build/platform/yfm")

		if docs.plantuml {
			resourcePeers = append(resourcePeers, "build/platform/java/jdk/jdk21")
		}
	}

	for _, peer := range resourcePeers {
		if !peerYaMakeExists(ctx.fs, source(peer)) {
			continue
		}

		res := genModule(ctx, ModuleInstance{Path: source(peer), Kind: KindLib, Language: instance.Language, Platform: instance.Platform})

		globals = append(globals, res.ResourceGlobalClosure...)
	}

	d.tc = resolveModuleToolchain(ctx, globals, instance.Platform.ClangVer)

	if d.tc.Python3 == 0 {
		d.tc.Python3 = build("resources/", resourcePatternYMakePython3, "/bin/python3")
	}

	d.cc.TC = d.tc
	d.scanCtx = newModuleScanContext(ctx, instance, d, nil, nil, nil, nil)
	e.emitFromSandboxes()

	e.emitDeclaredProducers(nil)

	generated := dedup(e.outs)

	for _, out := range generated {
		e.codegen.use(out)
	}

	peers = dedup(peers)

	name := realPrjName(module)

	if len(d.moduleStmt.Args) > 0 {
		name = d.moduleStmt.Args[0].string()
	}

	res := &ModuleEmitResult{ModuleStmtName: d.moduleStmt.Name, ResourceGlobalClosure: globals}

	if d.moduleStmt.Name == tokDocsLibrary {
		out := build(module, "/", name, ".docslib")
		ref := e.docsLibrary(out, generated, false)

		res.ARRef, res.ARPath, res.LDRef, res.LDPath = ref, ptr(out), ref, ptr(out)
		res.DocsClosure = append(append([]VFS{}, peers...), out)
	} else {
		external := build(module, "/__docs_external.docslib")
		externalRef := e.docsLibrary(external, generated, false)
		internal := build(module, "/__docs_internal.docslib.fake")

		e.docsLibrary(internal, generated, true)

		pre := build(module, "/_preprocessed.tar.gz")

		e.docsRender(pre, append(append([]VFS{}, peers...), internal), false)

		book := build(module, "/", name, ".tar.gz")
		bookRef := e.docsRender(book, []VFS{pre}, true)

		res.ARRef, res.ARPath = externalRef, ptr(external)
		res.LDRef, res.LDPath = bookRef, ptr(book)
		res.isPROGRAM = true
		res.DocsClosure = append(append([]VFS{}, peers...), external)
	}

	for _, kind := range []ModuleKind{KindLib, KindBin} {
		for _, demand := range []ModuleDemand{demandNone, demandSelf, demandLinked} {
			alias := instance

			alias.Kind, alias.Demand = kind, demand
			ctx.memoPut(alias, res)
		}
	}

	return res
}

func (e *EmitContext) docsCmd(script string, args ...string) Cmd {
	na := e.ctx.na
	head := []ANY{e.d.tc.Python3.any(), source("build/scripts/", script).any()}

	head = append(head, internAnys(args)...)

	return Cmd{CmdArgs: na.chunkList(na.anyList(head...)), Env: envVarsVCS}
}

func docsScriptInputs(scripts ...string) []VFS {
	inputs := make([]VFS, 0, len(scripts))

	for _, script := range scripts {
		inputs = append(inputs, source("build/scripts/", script))
	}

	return inputs
}

func (e *EmitContext) docsNode(cmds []Cmd, inputs, outputs, needs []VFS, render, module bool, env EnvVars, resources []STR) NodeRef {
	ctx, reg, na := e.ctx, e.codegen, e.ctx.na
	ref := ctx.emit.reserve()

	needs = append([]VFS{}, needs...)

	for _, input := range inputs {
		if input.isBuild() {
			needs = append(needs, input)
		}
	}

	needs = dedup(needs)

	var refs []NodeRef

	for _, p := range needs {
		if info := reg.lookup(p); info != nil {
			refs = append(refs, info.ProducerRef)
		}
	}

	node := Node{
		ExplicitInputChunks: 1, Module: module,
		Cmds: na.cmdList(cmds...), Inputs: na.inputList(na.vfsList(dedup(inputs)...)),
		Outputs: na.vfsList(outputs...), DepRefs: na.refList(dedupRefs(refs)...),
		KV: &docsKV, Env: env, Platform: e.instance.Platform,
		Resources: na.strs.list(resources...),
	}

	if render {
		node.Requirements = &docsRequirements
	}

	pending := na.pendingEmit(func() {
		for _, p := range needs {
			reg.use(p)
		}

		ctx.emit.emitReservedNode(node, ref)
	})

	for _, out := range outputs {
		reg.register(GeneratedFileInfo{OutputPath: out, OwnerModule: e.instance.Path, ProducerRef: ref, OnUse: pending})
	}

	return ref
}

func (e *EmitContext) emitDocsCopy(c DocsCopy) {
	module := e.instance.Path.relString()
	dst := buildJoinClean(module, c.namespace)
	args := []string{"--source-root", "$(S)", "--build-root", "$(B)", "--src-dir", c.from, "--dst-dir", dst.string()}

	args = append(args, c.files...)

	inputs := docsScriptInputs("copy_docs_files.py", "process_command_files.py")

	var outputs []VFS

	for _, file := range c.files {
		inputs = append(inputs, docsSource(strings.TrimRight(c.from, "/")+"/"+file))
		outputs = append(outputs, buildJoinClean(dst.relString(), file))
	}

	e.docsNode([]Cmd{e.docsCmd("copy_docs_files.py", args...)}, inputs, outputs, nil, false, false, envVarsVCS, usesPython3)
	e.outs = append(e.outs, outputs...)
}

func (e *EmitContext) docsLibrary(out VFS, generated []VFS, internal bool) NodeRef {
	docs := e.d.docs
	module := e.instance.Path.relString()
	bindir := build(module).string()

	var args []string

	for _, dir := range docs.dirs {
		ns := dir

		if internal {
			ns = "."
		}

		args = append(args, "--docs-dir", dir, ns)
	}

	ns := module

	if internal {
		ns = "."
	}

	args = append(args, "--bin-dir", bindir, ns)

	for _, out := range generated {
		args = append(args, out.string())
	}

	args = append(args, "--dest-dir", bindir+"/__s", "--source-root", "$(S)", "--build-root", "$(B)", "--srcs")

	for _, src := range docs.sources {
		args = append(args, src.string())
	}

	args = append(args, "--include-srcs")

	for _, src := range docs.includes {
		args = append(args, src.string())
	}

	if internal {
		args = append(args, "--skip-namespace", module+"/")
	}

	cmds := []Cmd{e.docsCmd("copy_docs_files_to_dir.py", args...), e.docsCmd("tar_sources.py", "--output", out.string(), "--input", bindir+"/__s")}
	inputs := append([]VFS{}, generated...)

	inputs = append(inputs, docsScriptInputs("copy_docs_files_to_dir.py", "process_command_files.py")...)
	inputs = append(inputs, docs.implicit...)
	inputs = append(inputs, docs.sources...)
	inputs = append(inputs, docs.includes...)
	inputs = append(inputs, source("build/scripts/tar_sources.py"))

	return e.docsNode(cmds, inputs, []VFS{out}, generated, false, true, envVarsVCS, usesPython3)
}

func (e *EmitContext) docsRender(out VFS, peers []VFS, book bool) NodeRef {
	docs := e.d.docs
	module := e.instance.Path.relString()
	bindir := build(module).string()
	extractArgs := []string{"--dest-dir", bindir + "/__s"}

	for _, peer := range peers {
		extractArgs = append(extractArgs, peer.relString())
	}

	if docs.plantuml && !book {
		extractArgs = append(extractArgs, "contrib/java/openjdk-fontconfig/"+realPrjName("contrib/java/openjdk-fontconfig")+".pkg.fake")
	}

	extract := e.docsCmd("extract_docs.py", extractArgs...)

	extract.Cwd = bld

	args := []string{"$(B)/resources/YFM_TOOL/yfm-docs", "--ignore-ya-make", "--input", bindir + "/__s", "--output", bindir + "/__docsbuild"}

	if docs.varsJSON != "" {
		args = append(args, "--vars", docs.varsJSON)
	}

	if book {
		args = append(args, "-f", "html", "--no-template")
	} else {
		args = append(args, "-f", "md", "--add-map-file", "--build-manifest", "--crawler-manifest")
	}

	config := docsSource(docs.config)

	args = append(args, "-j2", "--no-strict", "--allow-html", "--allow-custom-resources", "-e", "unarchive-includer", "-q", "--config", config.string())

	render := e.docsCmd("stdout2stderr.py", args...)

	render.Cwd = bld

	env := envVarsVCS
	resources := append([]STR{}, usesPython3...)

	resources = append(resources, internStr("YFM_TOOL"))

	if docs.plantuml && !book {
		env = e.ctx.na.envList(
			envVarsVCS[0],
			EnvVar{Name: envJavaPath, Value: internStr("$(B)/resources/JDK21/bin/java").any()},
			EnvVar{Name: envLang, Value: internStr("en_US.UTF-8").any()},
			EnvVar{Name: envLcAll, Value: internStr("C.UTF-8").any()},
			EnvVar{Name: envPlantumlPath, Value: internStr("contrib/tools/plantuml/plantuml.run.cp.jar").any()},
			EnvVar{Name: envJavaOptions, Value: internStr("-Dsun.awt.fontconfig=contrib/java/openjdk-fontconfig/fontconfig.properties -Djava.awt.headless=true").any()},
		)

		render.Env = env
		resources = append(resources, internStr("JDK21"))
	}

	cmds := []Cmd{extract, render, e.docsCmd("tar_sources.py", "--output", out.string(), "--input", bindir+"/__docsbuild")}
	inputs := docsScriptInputs("extract_docs.py", "process_command_files.py", "stdout2stderr.py")

	inputs = append(inputs, config, source("build/scripts/tar_sources.py"))

	outputs := []VFS{out}

	if book {
		pre := build(module, "/preprocessed.tar.gz")
		manifest := build(module, "/docs.manifest")
		link := e.docsCmd("fs_tools.py", "link_or_copy", "_preprocessed.tar.gz", pre.string())

		link.Cwd = build(module)
		cmds = append(cmds, link, e.docsCmd("docs_manifest.py", "--output", manifest.string(), "html="+baseName(out.relString()), "preprocessed=preprocessed.tar.gz"))
		inputs = append(inputs, docsScriptInputs("fs_tools.py", "docs_manifest.py")...)
		outputs = append(outputs, pre, manifest)
	}

	return e.docsNode(cmds, inputs, outputs, peers, true, true, env, resources)
}
