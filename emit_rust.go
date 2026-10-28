package main

import (
	"path"
	"strings"
)

var rustKV = KV{P: pkRS, PC: pcLightRed, ShowOut: true}

func (e *EmitContext) emitRustModule(result *ModuleEmitResult, peers []ResolvedPeer, globals []ResourceDecl) *ModuleEmitResult {
	ctx, instance, d, na := e.ctx, e.instance, e.d, e.ctx.na
	env := Environment{s: &e.frame.ifEnv}
	value := func(name string) string { return expandConfScalar(env.string(internEnv(name)), env) }
	values := func(name string) []string { return strings.Fields(value(name)) }
	host := envRustHost(env)
	kind := value("_RUST_KIND")
	name := path.Base(instance.Path.relString())

	if len(d.moduleStmt.Args) > 0 {
		name = d.moduleStmt.Args[0].string()
	}

	out := build(instance.Path.relString(), "/", env.string(envMODULE_PREFIX), name, env.string(envMODULE_SUFFIX))
	outputs := []VFS{out}

	globals = e.scriptModuleResources(globals)
	result.ResourceGlobalClosure = globals
	var direct, dependencies, native []VFS

	directPeers := map[VFS]bool{}

	for _, peer := range peers {
		if peer.result.ModuleStmtName == tokCrate && peer.result.ARPath != nil {
			directPeers[*peer.result.ARPath] = true
		}
	}

	for _, archive := range result.PeerArchiveClosurePaths {
		info := e.codegen.lookup(archive)

		if info != nil {
			switch info.ModuleType {
			case tokCrate:
				dependencies = append(dependencies, archive)

				if directPeers[archive] {
					direct = append(direct, archive)
				}

				continue
			case tokRustRuntime, tokCrateProcMacro, tokResourcesLibrary:
				continue
			}
		}

		native = append(native, archive)
	}

	inputs := append([]VFS{}, result.PeerArchiveClosurePaths...)

	for _, peer := range peers {
		if info := e.codegen.moduleInputs(moduleOutputPath(peer.result)); info != nil {
			inputs = append(inputs, info.Includes...)
		}
	}

	inputs = append(inputs, result.PeerSbomClosurePaths...)

	refs := append([]NodeRef{}, result.PeerArchiveClosureRefs...)

	refs = append(refs, result.PeerSbomClosureRefs...)

	if sbomActive(ctx, instance) && sbomQualifies(d) {
		result.SbomComponentRef, result.SbomComponentPath = e.emitSbomComponent(name)
		inputs = append(inputs, *result.SbomComponentPath)
		refs = append(refs, *result.SbomComponentRef)
	}

	script := buildScriptsRustPy

	inputs = append(inputs, script)
	var textInputs, textTemplates []VFS

	input := func(file string) VFS {
		p := e.moduleSourceVFS(internAny(file))

		inputs = append(inputs, p)
		textInputs = append(textInputs, p)

		if strings.HasSuffix(p.relString(), ".in") {
			textTemplates = append(textTemplates, p)
		}

		return p
	}

	root := input(value("_RUST_ROOT"))

	for _, field := range []string{"_RUST_SOURCES", "_RUST_INPUTS"} {
		for _, file := range values(field) {
			input(file)
		}
	}

	target := rustTarget(instance.Platform, false)

	if host {
		target = rustTarget(ctx.host, true)
	}

	opt, debug := "0", "2"

	switch instance.Platform.BuildType {
	case "release", "minsizerel":
		opt, debug = "3", "0"
	case "relwithdebinfo":
		opt = "3"
	}

	sanitizer := env.string(envSANITIZER_TYPE)

	if sanitizer != "address" && sanitizer != "memory" && sanitizer != "thread" && sanitizer != "leak" {
		sanitizer = ""
	}

	args := []ANY{d.tc.Python3.any(), script.any()}
	add := func(words ...string) { args = append(args, internAnys(words)...) }

	paths := func(files []VFS) {
		for _, file := range files {
			args = append(args, file.any())
		}
	}

	add("compile", "--rust-root", "$RUST_RESOURCE_GLOBAL", "--clang-root", "$CLANG20_RESOURCE_GLOBAL",
		"--source-root", "$(S)", "--build-root", "$(B)", "--manifest-dir", value("_RUST_MANIFEST_DIR"),
		"--kind", kind, "--mode", value("_RUST_MODE"), "--target", target, "--name", name,
		"--version", value("_RUST_VERSION"), "--edition", value("_RUST_EDITION"),
		"--opt-level", opt, "--debuginfo", debug, "--sanitizer="+sanitizer,
		"--crate-root", root.string(), "--output", out.string())

	if kind == "rlib" {
		stem := env.string(envMODULE_PREFIX) + name

		if host {
			stem += "-host"
		}

		metadata := build(instance.Path.relString(), "/", stem, ".rlib")
		info := build(instance.Path.relString(), "/", stem, ".build.tar")

		outputs = append(outputs, metadata, info)
		add("--metadata-output", metadata.string(), "--build-info-output", info.string())
	}

	if manifest := value("_RUST_MANIFEST"); manifest != "" {
		add("--manifest", input(manifest).string())
	}

	var toolRefs []NodeRef

	tool := func(dir string) VFS {
		ref, file := ctx.tool(internArg(dir))

		toolRefs = append(toolRefs, ref)
		inputs = append(inputs, file)

		return file
	}

	if dir := value("_RUST_BUILD_SCRIPT"); dir != "" {
		add("--build-script-program", tool(dir).string())
	}

	add("--features")
	add(values("_RUST_FEATURES")...)

	for _, flag := range values("_RUST_FLAGS") {
		add("--flag=" + flag)
	}

	add("--env")
	add(values("_RUST_ENV")...)

	for _, spec := range values("_RUST_BUILD_TOOLS") {
		name, dir, _ := strings.Cut(spec, "=")

		add(name + "=" + tool(dir).string())
	}

	add("--aliases")
	add(values("_RUST_ALIASES")...)
	var macros []VFS

	for _, dir := range values("_RUST_PROC_MACROS") {
		macros = append(macros, tool(dir))
	}

	add("--proc-macros")
	paths(macros)
	add("--direct")
	paths(direct)
	add("--dependencies")
	paths(dependencies)
	paths(macros)
	add("--native")
	paths(native)

	command := resourceGlobalArgumentValue(args, globals)

	ref := e.emitLazyModuleNode(Node{
		Module: true, Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(command.Args...)), Env: envVarsVCS}),
		Env:  envVarsVCS, Inputs: NodeInputs{
			files: na.rootInputList(na.vfsList(dedup(inputs)...)),
			roots: na.inputList(na.vfsList(script), na.vfsList(textInputs...)), text: na.vfsList(textInputs...),
		},
		Outputs: na.vfsList(outputs...), KV: &rustKV,
		DepRefs: na.uniqueRefs(refs), ForeignDepRefs: na.uniqueRefs(toolRefs),
		Resources: na.strs.list(dedup(append(command.Resources, strYMakePython3Name))...), ResourceGlobals: globals,
	}, nil)

	if len(textTemplates) > 0 {
		e.codegen.setModuleInputs(out, d.moduleStmt.Name, na.vfsList(textTemplates...), nil, nil, nil, nil, false)
	}

	result.ARRef, result.LDRef = ref, ref
	result.ARPath, result.LDPath = ptr(out), ptr(out)
	result.ArchiveFile = out
	result.isPROGRAM = kind == "bin" || kind == "proc-macro"

	if d.moduleStmt.Name == tokRustRuntime {
		result.PeerArchiveClosurePaths, result.PeerArchiveClosureRefs = nil, nil
		result.PeerSbomClosurePaths, result.PeerSbomClosureRefs = nil, nil
	}

	return result
}
