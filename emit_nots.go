package main

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

var notsBaseExcludes = []string{"a.yaml", ".npmrc", "package.json", "pnpm-lock.yaml", "output.tar", "output.tar.uuid", "node_modules.layer", "workspace_node_modules.tar", "(.idea|.vscode|node_modules)/**/*"}

const notsBuilderDir = "devtools/frontend_build_platform/nots/builder"

type NotsModule struct {
	Checks            []NotsCheck
	TestDirs          []string
	TestConfig        string
	DependsOnBuild    bool
	Kind              TOK
	Hermetic          bool
	LocalCLI          bool
	Bundle            bool
	ProductionBundle  bool
	BuildScript       string
	BuildCommand      string
	Outputs           []string
	Excludes          []string
	Configs           []string
	VCSInfo           string
	OutputPrefix      string
	HasOutputPrefix   bool
	IncludeTags       []string
	ProtoOptions      []string
	ProtoPeers        []ANY
	ProtoPackage      string
	ProtoDeps         string
	AutoProto         bool
	ProtoNonRecursive bool
	ESLintConfig      string
	Typecheck         bool
	TypecheckConfig   string
	StylelintConfig   string
	BiomeConfig       string
	TestData          []string
	ConfigChanged     bool
}

type NotsCheck struct {
	Script  string
	Command string
	Lint    bool
	Medium  bool
}

func isNotsModule(name TOK) bool {
	switch name {
	case tokTsPackage, tokTsLibrary, tokTsNext, tokTsVite, tokTsTsc, tokTsTestFor, tokTsTestJestFor, tokTsTestVitestFor:
		return true
	}

	return false
}

func newNotsModule(kind TOK, env Environment) *NotsModule {
	c := &NotsModule{Kind: kind, Configs: []string{"tsconfig.json"}, LocalCLI: env.bool(envTsLocalCli)}

	c.Hermetic = env.bool(envAutocheck) && !env.bool(envTsDisableHermeticNodeModules) && !c.LocalCLI

	switch kind {
	case tokProtoLibrary:
		c.ProtoPackage, c.ProtoDeps = "@yandex-proto/*", "library/typescript/ts-proto-deps"
	case tokTsTsc:
		c.BuildScript = "nots:build"
		c.setTSCCommand()
	case tokTsNext:
		c.BuildScript, c.BuildCommand, c.Outputs = "nots:build", "next build", []string{".next"}
	case tokTsVite:
		c.BuildScript, c.BuildCommand, c.Outputs = "nots:build", "vite build --config vite.config.ts", []string{"dist"}
	}

	return c
}

func (c *NotsModule) setTSCCommand() {
	var commands []string

	for _, config := range c.Configs {
		commands = append(commands, "tsc --project "+config+" --incremental false --composite false --pretty")
	}

	c.BuildCommand = strings.Join(commands, " && ")
}

func applyNotsStmt(v UnknownStmt, c *NotsModule) bool {
	name := v.Raw.string()

	if v.Name != tokInvalid {
		name = v.Name.string()
	}

	args := make([]string, len(v.Args))

	for i, a := range v.Args {
		args[i] = a.string()
	}

	switch name {
	case "TS_TEST_SRCS":
		c.TestDirs = args
	case "TS_TEST_CONFIG":
		if len(args) > 0 {
			c.TestConfig = args[0]
		}
	case "TS_ESLINT_CONFIG":
		if len(args) > 0 {
			c.ESLintConfig = args[0]
		}
	case "TS_TYPECHECK":
		c.Typecheck = true

		if len(args) > 0 {
			c.TypecheckConfig = args[0]
		}
	case "NO_TS_TYPECHECK":
		c.Typecheck = false
	case "TS_STYLELINT":
		if len(args) > 0 {
			c.StylelintConfig = args[0]
		}
	case "TS_BIOME":
		if len(args) > 0 {
			c.BiomeConfig = args[0]
		}
	case "TS_TEST_DATA":
		c.TestData = append(c.TestData, args...)
	case "INCLUDE_TAGS":
		c.IncludeTags = append(c.IncludeTags, args...)
	case "TS_PROTO_OPT":
		c.ProtoOptions = append(c.ProtoOptions, args...)
	case "TS_PROTO_PACKAGE_NAME":
		if len(args) > 0 {
			c.ProtoPackage = args[0]
		}
	case "TS_PROTO_NON_RECURSIVE":
		c.ProtoNonRecursive = true
	case "TS_TEST", "TS_LINT":
		if len(args) == 0 {
			throwFmt("%s requires a script name", name)
		}

		check := NotsCheck{Lint: name == "TS_LINT"}

		var command []string

		for _, a := range args {
			if a == "TIMEOUT_MEDIUM" {
				check.Medium = true
			} else if check.Script == "" {
				check.Script = a
			} else {
				command = append(command, a)
			}
		}

		check.Command = strings.Join(command, " ")
		c.Checks = append(c.Checks, check)
	case "TS_TEST_DEPENDS_ON_BUILD":
		c.DependsOnBuild = true
	case "DISABLE_HERMETIC_NODE_MODULES":
		c.Hermetic = false
	case "WITH_NODE_MODULES":
		c.Bundle, c.ProductionBundle = true, slices.Contains(args, "PROD")
	case "TS_BUILD_SCRIPT":
		if len(args) > 0 {
			c.BuildScript, c.BuildCommand = args[0], strings.Join(args[1:], " ")
		}
	case "TS_BUILD_OUTPUTS", "TS_NEXT_OUTPUT", "TS_VITE_OUTPUT", "VITE_OUTPUT":
		c.Outputs = args
	case "TS_EXCLUDE_FILES_GLOB":
		c.Excludes = append(c.Excludes, args...)
	case "TS_CONFIG":
		c.Configs = args
		c.ConfigChanged = true

		if c.Kind == tokTsTsc {
			c.setTSCCommand()
		}
	case "TS_VITE_CONFIG":
		var commands []string

		for _, config := range args {
			commands = append(commands, "vite build --config "+config)
		}

		c.BuildCommand = strings.Join(commands, " && ")
	case "TS_NEXT_BUILD_OPTIONS":
		c.BuildCommand = strings.TrimSpace("next build " + strings.Join(args, " "))
	case "TS_NEXT_CONFIG", "TS_FILES", "TS_FILES_GLOB", "USE_LEGACY_PNPM_VIRTUAL_STORE", "INJECT_PEERS":
	case "VCS_INFO_FILE":
		c.VCSInfo = "vcs_info.json"

		if len(args) == 2 && args[0] == "FILE" {
			c.VCSInfo = args[1]
		}
	case "TS_OUTPUT_PREFIX":
		c.HasOutputPrefix = true
		c.OutputPrefix = strings.Join(args, " ")
	default:
		return false
	}

	return true
}

func (e *EmitContext) notsResource(dir, name string) ResourceDecl {
	result := genModule(e.ctx, e.derivePeerInstance(dir))

	for _, decl := range result.ResourceGlobalClosure {
		if decl.Name.string() == name {
			return decl
		}
	}

	throwFmt("nots: %s did not declare resource %s", dir, name)

	return ResourceDecl{}
}

func (e *EmitContext) notsPeerInstance(dir string) ModuleInstance {
	instance := e.derivePeerInstance(dir)

	instance.Language = LangTS

	return instance
}

func (e *EmitContext) notsBaseArgs(pj map[string]any) ([]string, []ResourceDecl, ResourceDecl) {
	return e.notsBaseArgsFor(pj, e.d.nots)
}

func (e *EmitContext) notsBaseArgsFor(pj map[string]any, c *NotsModule) ([]string, []ResourceDecl, ResourceDecl) {
	pm := e.ctx.parsers
	version := pm.notsVersion("nodejs", notsString(notsObject(pj["engines"])["node"]))
	parts := notsVersionParts(version)

	if parts[0] < 20 {
		throwFmt("nots: Node.js %s is unsupported", version)
	}

	node := e.notsResource("build/platform/nodejs/"+version, "NODEJS_"+strings.ReplaceAll(version, ".", "_"))
	pnpm := e.notsResource("build/external_resources/pnpm", "PNPM")
	nodeResources := genModule(e.ctx, e.derivePeerInstance("build/platform/nodejs/"+version))
	globals := append(slices.Clone(nodeResources.ResourceGlobalClosure), pnpm)
	local := "no"

	if c.LocalCLI {
		local = "yes"
	}

	args := []string{"--arcadia-root", "$(S)", "--arcadia-build-root", "$(B)", "--moddir", e.instance.Path.relString(), "--local-cli", local, "--nodejs-bin", node.Value.string() + "/node", "--pm-script", pnpm.Value.string() + "/node_modules/pnpm/dist/pnpm.cjs", "--pm-type", "pnpm"}

	if parts[0] >= 25 && e.instance.Platform.OS == OSLinux {
		atomic := e.notsResource("build/platform/nodejs/libatomic", "LIBATOMIC_1_2_0")

		globals = append(globals, atomic)
		args = append(args, "--ld-library-path", atomic.Value.string())
	}

	if c.Hermetic {
		args = append(args, "--hermetic-node-modules", "yes")

		if e.instance.Platform.OS == OSLinux && e.instance.Platform.ISA == ISAX8664 {
			squash := e.notsResource("devtools/frontend_build_platform/nots/squashfs", "SQUASHFS_TOOLS")

			globals = append(globals, squash)
			args = append(args, "--squashfs-tools-path", squash.Value.string())
		}
	}

	if pj["usePrebuilder"] == true {
		v := pm.notsVersion("@yatool/prebuilder", "")
		name := "YATOOL_PREBUILDER_" + strings.ReplaceAll(v, ".", "_")
		prebuilder := e.notsResource("build/external_resources/yatool_prebuilder/"+v, name)

		globals = append(globals, prebuilder)
		args = append(args, "--yatool-prebuilder-path", prebuilder.Value.string()+"/node_modules/@yatool/prebuilder")
	}

	return args, globals, node
}

func (e *EmitContext) notsRequirements() *Requirements {
	req := &Requirements{CPU: 4, RAM: 32, Network: nwRestricted}

	if e.d.nots.Hermetic {
		req.HasRAMDisk, req.RAMDisk = true, 4
	}

	return req
}

func (e *EmitContext) emitNotsNode(n Node) NodeRef {
	ref := e.emitNode(n)

	for _, out := range n.Outputs {
		e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref, ProducerMainOut: n.Outputs[0]})
	}

	return ref
}

type NotsPrepared struct {
	Head            []string
	Globals         []ResourceDecl
	Nodejs          ResourceDecl
	ResourceNames   []STR
	ToolRef         NodeRef
	ToolPath        VFS
	HasDependencies bool
	Schema          bool
	SourceFiles     []VFS
	SourcesLoaded   bool
}

func notsResult(output VFS, ref NodeRef, c *NotsModule, globals []ResourceDecl) *ModuleEmitResult {
	return &ModuleEmitResult{ARRef: ref, ARPath: ptr(output), LDRef: ref, LDPath: ptr(output), ModuleStmtName: c.Kind, ResourceGlobalClosure: globals}
}

func (e *EmitContext) emitNotsModule() *ModuleEmitResult {
	switch e.d.nots.Kind {
	case tokTsTestFor, tokTsTestJestFor, tokTsTestVitestFor:
		return e.emitNotsTest()
	}

	if e.instance.Language == LangTSPrepare {
		return e.emitNotsPrepare()
	}

	return e.emitNotsBuild()
}

func (e *EmitContext) emitNotsPrepare() *ModuleEmitResult {
	ctx, dir, c := e.ctx, e.instance.Path.relString(), e.d.nots
	pm, na := ctx.parsers, ctx.na
	output := build(dir, "/package.json")

	if info := e.codegen.use(output); info != nil {
		return notsResult(output, info.ProducerRef, c, info.Nots.Globals)
	}

	var pj map[string]any

	if !c.AutoProto {
		pj = pm.notsJSON(dir + "/package.json")
	}

	base, globals, nodejs := e.notsBaseArgs(pj)

	var resourceNames []STR

	for _, decl := range globals {
		resourceNames = append(resourceNames, decl.Name)
	}

	var peerRefs []NodeRef
	var peerInputs []VFS
	var prepInputs []VFS

	addPeer := func(result *ModuleEmitResult) {
		globals = append(globals, result.ResourceGlobalClosure...)

		if result.ARPath == nil {
			return
		}

		if info := e.codegen.lookup(*result.ARPath); info != nil {
			peerRefs = append(peerRefs, info.GeneratorRefs...)
			peerInputs = append(peerInputs, info.ClosureLeaves...)
		}

		peerRefs = append(peerRefs, result.ARRef)
		peerInputs = append(peerInputs, *result.ARPath)
	}

	for _, peer := range pm.notsPeers(dir, pj) {
		result := e.genPeerModule(e.notsPeerInstance(peer))

		addPeer(result)

		prepInputs = append(prepInputs, build(peer, "/pnpm-workspace.yaml"))
	}

	var explicitPeers []ANY

	if c.Kind != tokProtoLibrary {
		explicitPeers = e.d.peerdirs
	} else if c.ProtoNonRecursive {
		explicitPeers = c.ProtoPeers
	}

	if c.AutoProto {
		explicitPeers = append(slices.Clone(explicitPeers), internStr(c.ProtoDeps).any())
	}

	for _, peer := range explicitPeers {
		result := e.genPeerModule(e.notsPeerInstance(peer.string()))

		addPeer(result)
	}

	peerRefs = dedupRefs(peerRefs)
	peerInputs = dedup(peerInputs)

	toolRef, toolPath := ctx.tool(internArg(notsBuilderDir))

	prepInputs = append(prepInputs, toolPath)

	if c.Hermetic {
		prepInputs = append(prepInputs, peerInputs...)
	}

	head := append([]string{toolPath.string()}, base...)
	prepArgs := append(slices.Clone(head), "prepare-deps", "--tarballs-store", "__tarballs__")

	if !c.AutoProto {
		prepInputs = append(prepInputs, source(dir, "/package.json"))
	}

	hasDeps := notsHasDependencies(pj)

	if hasDeps || ctx.fs.isFilePathClean(dir+"/pnpm-lock.yaml") {
		prepInputs = append(prepInputs, source(dir, "/pnpm-lock.yaml"))
	}

	if config := notsString(notsObject(pj["nots"])["commonConfigPath"]); config != "" {
		prepInputs = append(prepInputs, source(path.Join(dir, config)))
	}

	if c.Hermetic {
		for _, patch := range notsObject(notsObject(pj["pnpm"])["patchedDependencies"]) {
			prepInputs = append(prepInputs, source(path.Join(dir, notsString(patch))))
		}
	}

	prepOutputs := []VFS{build(dir, "/package.json"), build(dir, "/pnpm-lock.yaml"), build(dir, "/pnpm-workspace.yaml")}

	if c.Hermetic {
		prepOutputs = append(prepOutputs, build(dir, "/node_modules.layer"))
	}

	var uris []string
	var tarballs []string

	if hasDeps {
		if !c.LocalCLI {
			for _, pkg := range pm.notsLockfile(dir + "/pnpm-lock.yaml").Packages {
				uris = append(uris, pkg.URI)
				tarballs = append(tarballs, pkg.Path)
			}
		}

		prepArgs = append(prepArgs, "--resource-root", "$(RESOURCE_ROOT)")
	}

	if c.AutoProto {
		if c.Hermetic {
			for _, pkg := range pm.notsLockfile(c.ProtoDeps + "/pnpm-lock.yaml").Packages {
				uris = append(uris, pkg.URI)
			}

			prepArgs = append(prepArgs, "--resource-root", "$(RESOURCE_ROOT)")
		}

		prepArgs = append(prepArgs, "--ts-proto-auto-deps-path", c.ProtoDeps, "--ts-proto-auto-package-name", c.ProtoPackage)

		if c.ProtoNonRecursive && len(c.ProtoPeers) > 0 {
			peers := anyStrs(c.ProtoPeers)

			slices.Sort(peers)
			prepArgs = append(prepArgs, "--ts-proto-workspace-peers")
			prepArgs = append(prepArgs, slices.Compact(peers)...)
		}
	}

	slices.Sort(uris)
	slices.Sort(tarballs)

	for _, tarball := range tarballs {
		prepOutputs = append(prepOutputs, build(dir, "/__tarballs__/", tarball))
	}

	prep := Node{Platform: e.instance.Platform, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(appendInternAnys(nil, prepArgs)...)), Env: envVarsVCS}), Env: envVarsVCS, Inputs: na.inputList(na.vfsList(prepInputs...)), Outputs: na.vfsList(prepOutputs...), KV: &KV{P: pkTSDep, PC: pcMagenta}, KVExts: []KVExt{{Key: "text", Val: dir}}, ForeignDepRefs: na.refList(toolRef), Requirements: e.notsRequirements(), Resources: resourceNames, ResourceURIs: make([]STR, len(uris))}

	for i, uri := range uris {
		prep.ResourceURIs[i] = internStr(uri)
	}

	if c.Hermetic {
		prep.DepRefs = na.refList(peerRefs...)
	}

	prepRef := e.emitNotsNode(prep)

	globals = e.notsTestGlobals(globals)

	state := &NotsPrepared{Head: head, Globals: globals, Nodejs: nodejs, ResourceNames: resourceNames, ToolRef: toolRef, ToolPath: toolPath, HasDependencies: hasDeps, Schema: e.d.moduleStmt.Schema}
	info := e.codegen.lookup(output)

	info.Nots = state
	info.GeneratorRefs = peerRefs
	info.ClosureLeaves = peerInputs

	return notsResult(output, prepRef, c, globals)
}

func (e *EmitContext) emitNotsBuild() *ModuleEmitResult {
	ctx, dir, c := e.ctx, e.instance.Path.relString(), e.d.nots
	na := ctx.na
	output := build(dir, "/output.tar")
	instance := e.instance

	instance.Language = LangTSPrepare

	prepare := e.genPeerModule(instance)

	if prepare.ARPath == nil {
		throwFmt("nots: %s requires the TS_PREPARE_DEPS variant", dir)
	}

	if info := e.codegen.use(output); info != nil {
		result := notsResult(output, info.ProducerRef, c, info.Nots.Globals)

		result.TestSuites = e.notsBuildSuites(dir, info.Nots, e.notsFiles(dir, c.Outputs, c.Excludes))

		return result
	}

	prepRef := prepare.ARRef
	state := e.codegen.lookup(*prepare.ARPath).Nots
	head, globals, resourceNames, peerRefs, toolRef, hasDeps := state.Head, state.Globals, state.ResourceNames, e.codegen.lookup(*prepare.ARPath).GeneratorRefs, state.ToolRef, state.HasDependencies
	autotar := buildScriptsAutotarGendirsPy
	cmds := []Cmd{{CmdArgs: na.chunkList(na.anyList(wrapccPython3STR.any(), autotar.any(), strUnpack.any(), strExt.any(), strGentar.any())), Env: envVarsVCS, Cwd: build(dir)}}
	inputs := []VFS{autotar, state.ToolPath, *prepare.ARPath}

	inputs = append(inputs, e.codegen.lookup(*prepare.ARPath).ClosureLeaves...)

	buildLeaves := append(slices.Clone(e.codegen.lookup(*prepare.ARPath).ClosureLeaves), *prepare.ARPath)
	outputs := []VFS{output}

	if c.VCSInfo != "" && c.Kind != tokTsPackage {
		vcs := ldVcsInfoVFS
		vcsOut := build(dir, "/", c.VCSInfo)

		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(wrapccPython3STR.any(), vcs.any(), strOutputJson.any(), strVcsVcsJson.any(), vcsOut.any())), Env: envVarsVCS})
		inputs = append(inputs, vcs)
		outputs = append(outputs, vcsOut)
	}

	buildHead := slices.Clone(head)

	if c.Bundle && hasDeps {
		buildHead = append(buildHead, "--nm-bundle", "yes")

		if c.ProductionBundle {
			buildHead = append(buildHead, "--nm-bundle-prod", "yes")
		}
	}

	kind, command := pkTSLib, "build-library"

	if c.Kind == tokTsPackage {
		kind, command = pkTSPkg, "build-package"
	} else if c.Kind == tokProtoLibrary {
		kind, command = pkTSProto, "build-ts-proto"
	}

	args := append(buildHead, command)

	if c.HasOutputPrefix {
		prefix := c.OutputPrefix

		if prefix == "" {
			prefix = dir
		}

		args = append(args, "--output-prefix="+prefix)
	}

	args = append(args, "--output-file", output.string(), "--vcs-info", c.VCSInfo)
	outputs = append(outputs, build(dir, "/output.tar.uuid"))

	foreignRefs := []NodeRef{toolRef}

	if c.Kind == tokProtoLibrary {
		protoArgs, protoInputs, protoRefs, protoLeaves, protocRef, protoGlobals := e.notsProtoInputs()
		builtState := *state

		globals = e.notsTestGlobals(append(slices.Clone(globals), protoGlobals...))
		builtState.Globals = globals
		state = &builtState
		foreignRefs = append(foreignRefs, protocRef)
		args = append(args, protoArgs...)
		inputs = append(inputs, protoInputs...)
		peerRefs = append(slices.Clone(peerRefs), protoRefs...)
		inputs = append(inputs, protoLeaves...)
		buildLeaves = append(buildLeaves, protoLeaves...)
	} else {
		if len(c.Outputs) > 0 {
			args = append(args, "--outputs")
			args = append(args, c.Outputs...)
		}

		args = append(args, "--exclude-globs")
		args = append(args, notsBaseExcludes...)
		args = append(args, c.Excludes...)

		if c.Kind != tokTsPackage {
			args = append(args, "--build-script", c.BuildScript)

			if c.BuildCommand != "" {
				args = append(args, "--build-command", c.BuildCommand)
			}
		}
	}

	sourceFiles := e.notsFiles(dir, c.Outputs, c.Excludes)

	inputs = append(inputs, sourceFiles...)

	if c.Kind != tokProtoLibrary {
		inputs = append(inputs, source(dir, "/ya.make"), source(dir, "/package.json"))
	}

	inputs = dedup(inputs)

	if c.Bundle && hasDeps {
		outputs = append(outputs, build(dir, "/workspace_node_modules.tar"))
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(appendInternAnys(nil, args)...)), Env: envVarsVCS})

	refs := append(slices.Clone(peerRefs), prepRef)

	if c.VCSInfo != "" {
		refs = append(refs, ctx.vcsRef)
	}

	n := Node{Platform: e.instance.Platform, Cmds: na.cmdList(cmds...), Env: envVarsVCS, Inputs: na.inputList(na.vfsList(inputs...)), Outputs: na.vfsList(outputs...), KV: &KV{P: kind, PC: pcMagenta}, ForeignDepRefs: na.refList(foreignRefs...), DepRefs: na.refList(dedupRefs(refs)...), Requirements: e.notsRequirements(), Resources: append(resourceNames, usesPython3...)}
	ref := e.emitNotsNode(n)
	info := e.codegen.lookup(output)

	info.Nots = state
	info.GeneratorRefs = dedupRefs(append(slices.Clone(peerRefs), prepRef))
	info.ClosureLeaves = dedup(buildLeaves)

	result := notsResult(output, ref, c, globals)

	result.TestSuites = e.notsBuildSuites(dir, state, sourceFiles)

	return result
}

func (e *EmitContext) emitNotsTest() *ModuleEmitResult {
	dir, c, na := e.instance.Path.relString(), e.d.nots, e.ctx.na
	args := e.d.moduleStmt.Args
	relative := false
	target := ""

	for _, arg := range args {
		if arg.string() == "RELATIVE" {
			relative = true
		} else {
			target = arg.string()
		}
	}

	if target == "" || target == "." || target == "./" || relative && c.Kind == tokTsTestFor {
		throwFmt("%s: %s requires a tested module path", dir, c.Kind.string())
	}

	if relative {
		target = path.Join(dir, target)
	}

	instance := e.derivePeerInstance(target)

	if c.Kind == tokTsTestFor {
		instance.Language = LangTSPrepare
	}

	result := e.genPeerModule(instance)
	refs := []NodeRef{result.ARRef}

	var inputs []VFS

	if result.ARPath != nil {
		inputs = append(inputs, *result.ARPath)

		if info := e.codegen.lookup(*result.ARPath); info != nil {
			refs = append(refs, info.GeneratorRefs...)
			inputs = append(inputs, info.ClosureLeaves...)
		}
	}

	name := strings.ReplaceAll(archiveTailFor(dir), "/", "-")
	kind := pkTSJest

	if c.Kind == tokTsTestFor {
		bridge := build(dir, "/", name, ".test_for.prepare_deps.fake")
		ref := e.emitNotsTouch(bridge, refs, inputs, &KV{P: pkUN, PC: pcLightCyan})

		refs = append(refs, ref)
		inputs = append(inputs, bridge)
		kind = pkTSTest

		if c.DependsOnBuild {
			instance.Language = LangCPP

			built := genModule(e.ctx, instance)

			refs = append(refs, built.ARRef)

			if built.ARPath != nil {
				inputs = append(inputs, *built.ARPath)
			}
		}
	} else {
		name += ".ts_test.fake"
		inputs = append(inputs, e.notsTestInputs(target)...)
	}

	output := build(dir, "/", name)
	ref := e.emitNotsTouch(output, na.refList(dedupRefs(refs)...), inputs, &KV{P: kind, PC: pcMagenta})
	emitted := notsResult(output, ref, c, result.ResourceGlobalClosure)

	emitted.TestSuites = e.notsForSuites(target, ref, result.ResourceGlobalClosure)

	return emitted
}

func (e *EmitContext) notsTestInputs(target string) []VFS {
	c, dir := e.d.nots, e.instance.Path.relString()

	var files []VFS

	testPattern := notsGlob("**/*.(test|spec).(ts|tsx|mts|cts|js|jsx|mjs|cjs)")
	mockPattern := notsGlob("**/__mocks__/*")
	hasTests := false

	for _, testDir := range c.TestDirs {
		base := path.Join(dir, testDir)

		e.ctx.fs.walk(base, func(rel string, isDir bool) bool {
			if isDir {
				return true
			}

			local := strings.TrimPrefix(rel, base+"/")
			isTest := testPattern.MatchString(local)

			if isTest || mockPattern.MatchString(local) {
				files = append(files, source(rel))
				hasTests = hasTests || isTest
			}

			return false
		})
	}

	if e.ctx.testMode && hasTests {
		config := path.Join(dir, c.TestConfig)

		if c.TestConfig == "" {
			name := "jest.config.js"

			if c.Kind == tokTsTestVitestFor {
				name = "vitest.config.ts"
			}

			config = path.Join(target, name)
		}

		files = append(files, source(config))
	}

	ctx := newScanContext(e.ctx.na, e.ctx.parsers, nil, nil, nil, nil)

	var inputs []VFS

	for _, file := range dedup(files) {
		inputs = append(inputs, file)

		for _, bucket := range e.scanner.walkClosure(file, ctx, scanDomainAux).bucketList() {
			inputs = append(inputs, bucket...)
		}
	}

	return dedup(inputs)
}

func (e *EmitContext) emitNotsTouch(output VFS, refs []NodeRef, inputs []VFS, kv *KV) NodeRef {
	na := e.ctx.na
	script := buildScriptsTouchPy

	return e.emitNotsNode(Node{
		Platform: e.instance.Platform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(wrapccPython3STR.any(), script.any(), output.any())), Env: envVarsVCS}),
		Env:      envVarsVCS, Inputs: na.inputList(na.vfsList(script), na.vfsList(dedup(inputs)...)), Outputs: na.vfsList(output), KV: kv,
		DepRefs: na.refList(dedupRefs(refs)...), Resources: usesPython3,
		Requirements: &Requirements{CPU: 1, RAM: 32, Network: nwRestricted},
	})
}

func notsGlob(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")

	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++

				if i+1 < len(pattern) && pattern[i+1] == '/' {
					b.WriteString("(?:.*/)?")
					i++
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '(', ')', '|':
			b.WriteByte(pattern[i])
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}

	b.WriteString("$")

	return regexp.MustCompile(b.String())
}

func notsAllSourceFiles(fs FS, dir string) []VFS {
	var files []VFS

	fs.walk(dir, func(rel string, isDir bool) bool {
		local := strings.TrimPrefix(rel, dir+"/")

		if isDir {
			return local != "node_modules" && local != ".idea" && local != ".vscode"
		}

		files = append(files, source(rel))

		return false
	})

	slices.SortFunc(files, func(a, b VFS) int { return strings.Compare(a.relString(), b.relString()) })

	return files
}
