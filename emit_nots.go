package main

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

const notsBuilderDir = "devtools/frontend_build_platform/nots/builder"

type NotsModule struct {
	Checks            []NotsCheck
	TestDirs          []string
	TestConfig        string
	DependsOnBuild    bool
	Kind              TOK
	Prepare           bool
	AfterBuildCommand string
	Hermetic          bool
	LocalCLI          bool
	Bundle            bool
	ProductionBundle  bool
	BuildScript       string
	BuildCommand      string
	Outputs           []string
	BaseExcludes      []string
	Excludes          []string
	Configs           []string
	VCSInfo           string
	BuildEnv          []string
	OutputPrefix      string
	HasOutputPrefix   bool
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
	case tokTsPackage, tokTsLibrary, tokTsTestFor, tokTsTestJestFor, tokTsTestVitestFor:
		return true
	}

	return isNotsLegacyModule(name)
}

func isNotsLegacyModule(name TOK) bool {
	switch name {
	case tokTsTsc, tokTsNext, tokTsVite, tokTsRspack:
		return true
	}

	return false
}

func newNotsModule(kind TOK, language Language, env Environment) *NotsModule {
	c := &NotsModule{Kind: kind, Configs: []string{"tsconfig.json"}, LocalCLI: env.bool(envTsLocalCli), Prepare: language == LangTSPrepare}

	c.BaseExcludes = strings.Fields(env.expand(env.string(envSetTsInputsBaseExcludes)))
	c.Hermetic = env.bool(envAutocheck) && !env.bool(envTsDisableHermeticNodeModules) && !c.LocalCLI

	switch kind {
	case tokProtoLibrary:
		c.ProtoPackage, c.ProtoDeps = "@yandex-proto/*", "library/typescript/ts-proto-deps"
	case tokTsTsc:
		c.BuildScript = "nots:build"
	case tokTsNext:
		c.BuildScript, c.BuildCommand, c.Outputs = "nots:build", "next build", []string{".next"}
	case tokTsVite:
		c.BuildScript, c.BuildCommand, c.Outputs = "nots:build", "vite build --config vite.config.ts", []string{"dist"}
	case tokTsRspack:
		c.BuildScript, c.BuildCommand, c.Outputs = "nots:build", "rspack --config rspack.config.js", []string{"bundle"}
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

func applyNotsStmt(v UnknownStmt, c *NotsModule, env Environment) bool {
	name := v.Raw.string()

	if v.Name != tokInvalid {
		name = v.Name.string()
	}

	args := make([]string, len(v.Args))

	for i, a := range v.Args {
		args[i] = a.string()
	}

	switch name {
	case "RUN_JAVASCRIPT_AFTER_BUILD":
		if c.Prepare {
			return true
		}

		if c.Kind == tokTsPackage || c.Kind == tokTsLibrary {
			throwFmt("%s does not support RUN_JAVASCRIPT_AFTER_BUILD", c.Kind.string())
		}

		if !isNotsLegacyModule(c.Kind) {
			return false
		}

		if len(args) == 0 {
			throwFmt("RUN_JAVASCRIPT_AFTER_BUILD requires a script")
		}

		command := []string{"node", args[0]}
		input := false

		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "IN":
				input = true
			case "OUTDIR":
				input = false

				if i+1 < len(args) {
					i++
					c.Outputs = append(c.Outputs, args[i])
				}
			default:
				if !input {
					command = append(command, args[i])
				}
			}
		}

		env.setString(envTsAfterBuildCommand, strings.Join(command, " "))
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
	case "TS_PROTO_OPT":
		c.ProtoOptions = append(c.ProtoOptions, args...)
	case "TS_PROTO_PACKAGE_NAME":
		if len(args) > 0 {
			c.ProtoPackage = args[0]
		}
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
	case "TS_BUILD_OUTPUTS", "TS_NEXT_OUTPUT", "TS_VITE_OUTPUT", "VITE_OUTPUT", "TS_RSPACK_OUTPUT":
		c.Outputs = args
	case "TS_EXCLUDE_FILES_GLOB":
		c.Excludes = append(c.Excludes, args...)
	case "TS_CONFIG":
		c.Configs = args
		c.ConfigChanged = true

	case "TS_VITE_CONFIG":
		var commands []string

		for _, config := range args {
			commands = append(commands, "vite build --config "+config)
		}

		c.BuildCommand = strings.Join(commands, " && ")
	case "TS_RSPACK_CONFIG":
		if len(args) > 0 {
			c.BuildCommand = "rspack --config " + args[0]
		}
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

func (c *NotsModule) finish(env Environment) {
	if c.Kind == tokTsTsc {
		c.setTSCCommand()
	}

	c.AfterBuildCommand = env.string(envTsAfterBuildCommand)
	c.ProtoNonRecursive = env.bool(envTsProtoNonRecursive)
	c.setupBuildEnv(env)
}

func (c *NotsModule) setupBuildEnv(env Environment) {
	listed := map[string]bool{}

	if names, _ := env.lookup("TS_BUILD_ENV"); names != "" && notsBuildEnvFor(env) {
		for _, name := range strings.Split(names, ",") {
			value, ok := env.lookup("TS_ENV_" + name)

			if !ok {
				throwFmt("Env var '%s' is provided in a list, but var value is not provided", name)
			}

			c.BuildEnv = append(c.BuildEnv, "--env", name+"="+value)
			listed[name] = true
		}
	}

	defaults, _ := env.lookup("TS_BUILD_ENV_DEFAULTS_LIST")
	sep, _ := env.lookup("TS_BUILD_ENV_DEFAULTS_LIST_SEP")

	for _, entry := range strings.Split(strings.TrimPrefix(defaults, "$TS_BUILD_ENV_DEFAULTS_LIST"), sep) {
		entry = strings.TrimSpace(entry)

		if entry == "" {
			continue
		}

		name, _, ok := strings.Cut(entry, "=")

		if !ok {
			throwFmt("TS_BUILD_ENV expects key=value, got %q", entry)
		}

		if !listed[name] {
			c.BuildEnv = append(c.BuildEnv, "--env", entry)
		}
	}
}

func notsBuildEnvFor(env Environment) bool {
	targets, ok := env.lookup("TS_BUILD_ENV_FOR")

	return !ok || slices.Contains(strings.Split(targets, ":"), env.string(envMODDIR))
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

	env := Environment{s: &e.frame.ifEnv}

	env.setString(envPnpmRoot, pnpm.Value.string())

	pmScript := env.expand(env.string(envPmScript))
	args := []string{"--arcadia-root", "$(S)", "--arcadia-build-root", "$(B)", "--moddir", e.instance.Path.relString(), "--local-cli", local, "--nodejs-bin", node.Value.string() + "/node", "--pm-script", pmScript, "--pm-type", "pnpm"}

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

func (e *EmitContext) emitNotsNode(n NodeSpec) NodeRef {
	env := Environment{s: &e.frame.ifEnv}
	tool := env.string(envNotsTool)

	for from := 0; ; {
		start, end, name := nextConfReference(tool, from, true)

		if start == end {
			break
		}

		from = end

		modifiers, _, _ := strings.Cut(tool[start:end], ":")

		if !slices.Contains(strings.Split(strings.TrimPrefix(modifiers, "${"), ";"), "input") {
			continue
		}

		for _, input := range strings.Fields(env.expand(env.string(internEnv(name)))) {
			n.Inputs = append(n.Inputs, e.ctx.na.vfsList(source(input)))
		}
	}

	if sbomActive(e.ctx, e.instance) {
		if ref, file := pythonToolchainSbomComponent(e.ctx, e.instance.Platform); ref != nil && file != nil {
			n.Inputs = append(n.Inputs, e.ctx.na.vfsList(*file))
			n.DepRefs = e.ctx.na.refList(append(slices.Clone(n.DepRefs), *ref)...)
		}
	}

	ref := e.emitNode(n)

	for _, out := range n.Outputs {
		e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, ProducerMainOut: n.Outputs[0]})
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
	var result *ModuleEmitResult

	switch e.d.nots.Kind {
	case tokTsTestFor, tokTsTestJestFor, tokTsTestVitestFor:
		result = e.emitNotsTest()
	default:
		if e.instance.Language == LangTSPrepare {
			result = e.emitNotsPrepare()
		} else {
			result = e.emitNotsBuild()
		}
	}

	result.TestSuites = append(result.TestSuites, e.styleSuites(result.ResourceGlobalClosure)...)

	return result
}

func (e *EmitContext) emitNotsPrepare() *ModuleEmitResult {
	ctx, dir, c := e.ctx, e.instance.Path.relString(), e.d.nots
	pm, na := ctx.parsers, ctx.na
	output := build(dir, "/package.json")

	if info := e.codegen.use(output); info != nil {
		return notsResult(output, info.ProducerRef, c, info.nots().Globals)
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
			peerInputs = append(peerInputs, info.notsInputs()...)
		}

		peerRefs = append(peerRefs, result.ARRef)
		peerInputs = append(peerInputs, *result.ARPath)
	}

	localPeers := pm.notsPeers(dir, pj)

	if c.Hermetic {
		ctx.recordConfiguration(e.instance, nil, localPeers)
	}

	for _, peer := range localPeers {
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
	prepToolIndex := len(prepInputs)

	prepInputs = append(prepInputs, toolPath)

	if c.Hermetic || c.AutoProto {
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
			values := anyStrs(c.ProtoPeers)
			peers := append([]string{"$_TS_PROTO_PEERS"}, values...)

			slices.Sort(peers)

			env := Environment{s: &e.frame.ifEnv}

			env.setString(envTsProtoPeers, strings.Join(values, " "))

			prepArgs = append(prepArgs, "--ts-proto-workspace-peers")
			prepArgs = append(prepArgs, strings.Fields(env.expand(strings.Join(slices.Compact(peers), " ")))...)
		}
	}

	slices.Sort(uris)
	slices.Sort(tarballs)

	for _, tarball := range tarballs {
		prepOutputs = append(prepOutputs, build(dir, "/__tarballs__/", tarball))
	}

	prep := NodeSpec{Node: Node{Platform: e.instance.Platform, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(appendInternAnys(nil, prepArgs)...)), Env: envVarsVCS}), Env: envVarsVCS, Inputs: na.rootInputList(na.vfsList(prepInputs...)), Outputs: na.vfsList(prepOutputs...), KV: &KV{P: pkTSDep, PC: pcMagenta}, KVExts: []KVExt{{Key: "text", Val: dir}}, ForeignDepRefs: na.refList(toolRef), Requirements: e.notsRequirements(), Resources: resourceNames, Extra: &NodeExtra{ResourceURIs: make([]STR, len(uris))}}, input: InputSpec{roots: na.inputList(prepInputs[:prepToolIndex], prepInputs[prepToolIndex+1:])}}

	for i, uri := range uris {
		prep.Extra.ResourceURIs[i] = internStr(uri)
	}

	if c.Hermetic || c.AutoProto {
		prep.DepRefs = na.refList(peerRefs...)
	}

	prepRef := e.emitNotsNode(prep)

	globals = e.notsTestGlobals(globals)

	state := &NotsPrepared{Head: head, Globals: globals, Nodejs: nodejs, ResourceNames: resourceNames, ToolRef: toolRef, ToolPath: toolPath, HasDependencies: hasDeps, Schema: e.d.moduleStmt.Schema}
	info := e.codegen.lookup(output)

	info.attributes.nots.put(info.id, state)
	info.GeneratorRefs = peerRefs
	info.attributes.notsInputs.put(info.id, peerInputs)

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
		result := notsResult(output, info.ProducerRef, c, info.nots().Globals)

		result.TestSuites = e.notsBuildSuites(dir, info.nots(), e.notsFiles(dir, c.Outputs, c.Excludes))

		return result
	}

	prepRef := prepare.ARRef
	state := e.codegen.lookup(*prepare.ARPath).nots()
	head, globals, resourceNames, peerRefs, toolRef, hasDeps := state.Head, state.Globals, state.ResourceNames, e.codegen.lookup(*prepare.ARPath).GeneratorRefs, state.ToolRef, state.HasDependencies
	autotar := buildScriptsAutotarGendirsPy
	cmds := []Cmd{{CmdArgs: na.chunkList(na.anyList(wrapccPython3STR.any(), autotar.any(), strUnpack.any(), strExt.any(), strGentar.any())), Env: envVarsVCS, Cwd: build(dir)}}
	inputs := []VFS{autotar, state.ToolPath, *prepare.ARPath}

	inputs = append(inputs, e.codegen.lookup(*prepare.ARPath).notsInputs()...)

	buildFrom := append([]VFS{autotar}, inputs[2:]...)
	buildLeaves := append(slices.Clone(e.codegen.lookup(*prepare.ARPath).notsInputs()), *prepare.ARPath)
	outputs := []VFS{output}

	if c.VCSInfo != "" && c.Kind != tokTsPackage {
		vcs := ldVcsInfoVFS
		vcsOut := build(dir, "/", c.VCSInfo)

		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(wrapccPython3STR.any(), vcs.any(), strOutputJson.any(), strVcsVcsJson.any(), vcsOut.any())), Env: envVarsVCS})
		inputs = append(inputs, vcs)
		buildFrom = append(buildFrom, vcs)
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
	args = append(args, c.BuildEnv...)

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
		buildFrom = append(buildFrom, protoInputs[1:]...)
		peerRefs = append(slices.Clone(peerRefs), protoRefs...)
		inputs = append(inputs, protoLeaves...)
		buildFrom = append(buildFrom, protoLeaves...)
		buildLeaves = append(buildLeaves, protoLeaves...)
	} else {
		if len(c.Outputs) > 0 {
			args = append(args, "--outputs")
			args = append(args, c.Outputs...)
		}

		args = append(args, "--exclude-globs")
		args = append(args, c.BaseExcludes...)
		args = append(args, c.Excludes...)

		if c.Kind != tokTsPackage {
			args = append(args, "--build-script", c.BuildScript)

			command := c.BuildCommand

			if c.AfterBuildCommand != "" {
				command += " && " + c.AfterBuildCommand
			}

			if command != "" {
				args = append(args, "--build-command", command)
			}
		}
	}

	sourceFiles := e.notsFiles(dir, c.Outputs, c.Excludes)

	inputs = append(inputs, sourceFiles...)
	buildFrom = append(buildFrom, sourceFiles...)

	if c.Kind != tokProtoLibrary {
		inputs = append(inputs, source(dir, "/ya.make"), source(dir, "/package.json"))
		buildFrom = append(buildFrom, source(dir, "/ya.make"), source(dir, "/package.json"))
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

	n := NodeSpec{Node: Node{Platform: e.instance.Platform, Cmds: na.cmdList(cmds...), Env: envVarsVCS, Inputs: na.rootInputList(na.vfsList(inputs...)), Outputs: na.vfsList(outputs...), KV: &KV{P: kind, PC: pcMagenta}, ForeignDepRefs: na.refList(foreignRefs...), DepRefs: na.refList(dedupRefs(refs)...), Requirements: e.notsRequirements(), Resources: append(resourceNames, usesPython3...)}, input: InputSpec{roots: na.inputList(na.vfsList(dedup(buildFrom)...)), text: na.vfsList(sourceFiles...)}}
	ref := e.emitNotsNode(n)
	info := e.codegen.lookup(output)

	info.attributes.nots.put(info.id, state)
	info.GeneratorRefs = dedupRefs(append(slices.Clone(peerRefs), prepRef))
	info.attributes.notsInputs.put(info.id, dedup(buildLeaves))

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
			inputs = append(inputs, info.notsInputs()...)
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
	} else {
		name += ".ts_test.fake"

		testInputs := e.notsTestInputs(target)

		inputs = append(inputs, testInputs...)
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

		view := e.scanner.walkClosure(file, ctx, scanDomainAux)

		for _, bucket := range view.bucketList() {
			inputs = append(inputs, bucket...)
		}
	}

	return dedup(inputs)
}

func (e *EmitContext) emitNotsTouch(output VFS, refs []NodeRef, inputs []VFS, kv *KV) NodeRef {
	na := e.ctx.na
	script := buildScriptsTouchPy

	return e.emitNotsNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(wrapccPython3STR.any(), script.any(), output.any())), Env: envVarsVCS}),
		Env:  envVarsVCS, Inputs: na.rootInputList(na.vfsList(dedup(inputs)...), na.vfsList(script)), Outputs: na.vfsList(output), KV: kv,
		DepRefs: na.refList(dedupRefs(refs)...), Resources: usesPython3,
		Requirements: &Requirements{CPU: 1, RAM: 32, Network: nwRestricted}},
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
