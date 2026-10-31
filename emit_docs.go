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

	resourcePeers := []string{"build/platform/python/ymake_python3"}

	for _, p := range d.peerdirs {
		peer := p.string()
		language := instance.Language

		switch ctx.moduleName(peer) {
		case tokDocs, tokDocsLibrary:
		case tokProtoLibrary:
			language = LangDocsProto
		case tokResourcesLibrary:
			resourcePeers = append(resourcePeers, peer)

			continue
		default:
			continue
		}

		res := e.genPeerModule(ModuleInstance{Path: source(peer), Kind: KindLib, Language: language, Platform: instance.Platform})

		peers = append(peers, res.DocsClosure...)
		globals = append(globals, res.ResourceGlobalClosure...)
	}

	if d.moduleStmt.Name == tokDocs {
		resourcePeers = append(resourcePeers, "build/platform/yfm")

		if docs.plantuml {
			resourcePeers = append(resourcePeers, "build/platform/java/jdk/jdk21")
		}
	}

	globals = e.scriptModuleResources(globals, resourcePeers...)

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

func (e *EmitContext) docsNode(cmds []Cmd, inputs, buildFrom, outputs, needs []VFS, render, module bool, env EnvVars, resources []STR, foreign ...NodeRef) NodeRef {
	na := e.ctx.na

	node := NodeSpec{Node: Node{Module: module, Cmds: na.cmdList(cmds...), Inputs: na.rootInputList(na.vfsList(dedup(inputs)...)), Outputs: na.vfsList(outputs...),
		KV: &docsKV, Env: env, Platform: e.instance.Platform,
		Resources: na.strs.list(resources...), ForeignDepRefs: na.refList(foreign...)}, input: InputSpec{roots: na.inputList(na.vfsList(dedup(buildFrom)...))},
	}

	if render {
		node.Requirements = &docsRequirements
	}

	return e.emitLazyModuleNode(node, needs)
}

func (e *EmitContext) emitDocsCopy(c DocsCopy) {
	module := e.instance.Path.relString()
	dst := buildJoinClean(module, c.namespace)
	args := []string{"--source-root", "$(S)", "--build-root", "$(B)", "--src-dir", c.from, "--dst-dir", build(module).string() + "/" + c.namespace}

	args = append(args, c.files...)

	inputs := scriptInputs("copy_docs_files.py", "process_command_files.py")

	var outputs []VFS

	for _, file := range c.files {
		inputs = append(inputs, docsSource(strings.TrimRight(c.from, "/")+"/"+file))
		outputs = append(outputs, buildJoinClean(dst.relString(), file))
	}

	e.docsNode([]Cmd{e.pythonScriptCmd("copy_docs_files.py", args...)}, inputs, inputs, outputs, nil, false, false, envVarsVCS, usesPython3)
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

	cmds := []Cmd{e.pythonScriptCmd("copy_docs_files_to_dir.py", args...), e.pythonScriptCmd("tar_sources.py", "--output", out.string(), "--input", bindir+"/__s")}
	inputs := append([]VFS{}, generated...)

	inputs = append(inputs, scriptInputs("copy_docs_files_to_dir.py", "process_command_files.py")...)
	inputs = append(inputs, docs.implicit...)
	inputs = append(inputs, docs.sources...)
	inputs = append(inputs, docs.includes...)
	inputs = append(inputs, buildScriptsTarSourcesPy)

	return e.docsNode(cmds, inputs, inputs, []VFS{out}, generated, false, true, envVarsVCS, usesPython3)
}

func (e *EmitContext) docsRender(out VFS, peers []VFS, book bool) NodeRef {
	docs := e.d.docs
	module := e.instance.Path.relString()
	bindir := build(module).string()
	extractArgs := []string{"--dest-dir", bindir + "/__s"}

	for _, peer := range peers {
		extractArgs = append(extractArgs, peer.relString())
	}

	var toolRef NodeRef
	var toolPath VFS

	if docs.plantuml && !book {
		fontModule := "contrib/java/openjdk-fontconfig"

		if peerYaMakeExists(e.ctx.fs, source(fontModule)) {
			res := e.genPeerModule(ModuleInstance{Path: source(fontModule), Kind: KindLib, Platform: e.instance.Platform})

			if res.LDPath != nil {
				peers = append(peers, *res.LDPath)
				extractArgs = append(extractArgs, res.LDPath.relString())
			}
		}

		toolModule := "contrib/tools/plantuml"

		if peerYaMakeExists(e.ctx.fs, source(toolModule)) {
			toolRef, toolPath = e.ctx.tool(internArg(toolModule))
			e.ctx.scannerForPlatform(e.ctx.host).codegen.use(toolPath)
		}
	}

	extract := e.pythonScriptCmd("extract_docs.py", extractArgs...)

	extract.Cwd = bld.cwd()

	args := []string{"$(B)/resources/YFM_TOOL/yfm-docs", "--ignore-ya-make", "--input", bindir + "/__s", "--output", bindir + "/__docsbuild"}

	if docs.varsJSON != "" {
		args = append(args, "--vars", docs.varsJSON)
	}

	if book {
		args = append(args, "-f", "html", "--no-template")
	} else {
		args = append(args, "-f", "md", "--add-map-file", "--build-manifest", "--crawler-manifest")
	}

	config := resolveSourceOrGeneratedVFS(e.ctx, e.instance, docs.config, e.d.srcDirs)

	args = append(args, "-j2", "--no-strict", "--allow-html", "--allow-custom-resources", "-e", "unarchive-includer", "-q", "--config", config.string())

	render := e.pythonScriptCmd("stdout2stderr.py", args...)

	render.Cwd = bld.cwd()

	env := envVarsVCS
	resources := append([]STR{}, usesPython3...)

	resources = append(resources, strYfmTool)

	if docs.plantuml && !book {
		env = e.ctx.na.envList(
			envVarsVCS[0],
			EnvVar{Name: envJavaPath, Value: strBResourcesJdk21BinJava.any()},
			EnvVar{Name: envLang, Value: strEnUsUtf8.any()},
			EnvVar{Name: envLcAll, Value: strCUtf8.any()},
			EnvVar{Name: envPlantumlPath, Value: strContribToolsPlantumlPlantumlRunCpJar.any()},
			EnvVar{Name: envJavaOptions, Value: strDsunAwtFontconfigContribJavaOpenjdkFontconfigFontconfigPropertiesDjavaAwtHeadlessTrue.any()},
		)

		render.Env = env
		resources = append(resources, strJdk21)
	}

	cmds := []Cmd{extract, render, e.pythonScriptCmd("tar_sources.py", "--output", out.string(), "--input", bindir+"/__docsbuild")}
	inputs := scriptInputs("extract_docs.py", "process_command_files.py", "stdout2stderr.py")

	inputs = append(inputs, config, buildScriptsTarSourcesPy)
	inputs = append(inputs, peers...)

	buildFrom := append([]VFS(nil), inputs...)

	if toolPath != 0 {
		inputs = append(inputs, toolPath)
	}

	outputs := []VFS{out}

	if book {
		pre := build(module, "/preprocessed.tar.gz")
		manifest := build(module, "/docs.manifest")
		link := e.pythonScriptCmd("fs_tools.py", "link_or_copy", "_preprocessed.tar.gz", pre.string())

		link.Cwd = build(module).cwd()
		cmds = append(cmds, link, e.pythonScriptCmd("docs_manifest.py", "--output", manifest.string(), "html="+baseName(out.relString()), "preprocessed=preprocessed.tar.gz"))
		inputs = append(inputs, scriptInputs("fs_tools.py", "docs_manifest.py")...)
		buildFrom = append(buildFrom, scriptInputs("fs_tools.py", "docs_manifest.py")...)
		outputs = append(outputs, pre, manifest)
	}

	var foreign []NodeRef

	if toolRef != 0 {
		foreign = append(foreign, toolRef)
	}

	return e.docsNode(cmds, inputs, buildFrom, outputs, peers, true, true, env, resources, foreign...)
}
