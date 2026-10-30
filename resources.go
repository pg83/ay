package main

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var (
	strLLDRootName      = internStr(resourcePatternLLDRoot)
	strYMakePython3Name = internStr(resourcePatternYMakePython3)
	usesPython3         = []STR{strYMakePython3Name}
	usesPython3JDK17    = []STR{strYMakePython3Name, internStr(resourcePatternJDK17)}
	usesPython3Clang16  = []STR{strYMakePython3Name, internStr(resourcePatternClang16)}
	resourcesKV         = KV{P: pkld, PC: pcLightBlue, ShowOut: true}
)

const (
	resourceGlobalSuffix = "_RESOURCE_GLOBAL"
	platformDefaultArch  = "x86_64"
)

type ResourceDecl struct {
	Name       STR
	URI        STR
	Descriptor STR
	FetchKey   STR
	GlobalVar  STR
	Value      VFS
	Token      STR
}

type ResourceDescriptor struct {
	Pattern     string               `json:"pattern,omitempty"`
	Platform    string               `json:"platform,omitempty"`
	URI         string               `json:"resource,omitempty"`
	StripPrefix uint32               `json:"strip_prefix,omitempty"`
	Resources   []ResourceDescriptor `json:"resources"`
}

type ResourceBundleEntry struct {
	URI         string `json:"uri"`
	StripPrefix uint32 `json:"strip_prefix,omitempty"`
}

func resourceDeclForName(name string) ResourceDecl {
	value := "$(B)/resources/" + name
	globalVar := name + resourceGlobalSuffix

	return ResourceDecl{
		Name:      internStr(name),
		GlobalVar: internStr(globalVar),
		Value:     build("resources/", name),
		Token:     internV(globalVar, "::", value),
	}
}

func makeResourceDecl(name, uri string) ResourceDecl {
	decl := resourceDeclForName(name)

	decl.URI = internStr(uri)
	decl.FetchKey = internV(name, "\x00", uri)

	return decl
}

func makeResourceDescriptorDecl(name string, descriptor ResourceDescriptor) ResourceDecl {
	if descriptor.Resources == nil && descriptor.StripPrefix == 0 {
		return makeResourceDecl(name, descriptor.URI)
	}

	descriptor.Pattern = name

	decl := resourceDeclForName(name)

	decl.Descriptor = internStr(string(throw2(json.Marshal(descriptor))))
	decl.FetchKey = internV(name, "\x00", decl.Descriptor.string())

	return decl
}

func hostPlatformKey(host *Platform) string {
	return string(host.OS) + "-" + isaPlatformKey(host.ISA)
}

func resourceJSONPlatformKey(env Environment) string {
	switch {
	case env.bool(envOS_DARWIN):
		if env.bool(envARCH_ARM64) {
			return "darwin-arm64"
		}

		return "darwin"
	case env.bool(envOS_WINDOWS):
		return "win32"
	default:
		if env.bool(envARCH_AARCH64) {
			return "linux-aarch64"
		}

		return "linux"
	}
}

func canonizePlatformKey(key string) string {
	key = strings.ToLower(key)

	os, arch, found := strings.Cut(key, "-")

	if !found || arch == "" || arch == platformDefaultArch {
		return os
	}

	return os + "-" + arch
}

func resolveResourceURIFromBundle(bundle map[string]ResourceBundleEntry, env Environment) (string, bool) {
	want := resourceJSONPlatformKey(env)
	keys := make([]string, 0, len(bundle))

	for k := range bundle {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		if canonizePlatformKey(k) == want {
			return bundle[k].URI, true
		}
	}

	return "", false
}

func isaPlatformKey(isa ISA) string {
	if isa == ISAAArch64 {
		return "aarch64"
	}

	return string(isa)
}

func resolveResourceDecls(fs FS, host *Platform, modulePath string, stmt *DeclareResourceStmt) []ResourceDecl {
	switch stmt.Macro {
	case tokDeclareExternalResource:

		out := make([]ResourceDecl, 0, len(stmt.Args)/2)

		for i := 0; i+1 < len(stmt.Args); i += 2 {
			out = append(out, makeResourceDecl(stmt.Args[i].string(), stmt.Args[i+1].string()))
		}

		return out
	case tokDeclareExternalHostResourcesBundle:

		bundle := map[string]ResourceBundleEntry{}
		i := 1

		for ; i+2 < len(stmt.Args); i += 3 {
			uri, platform := stmt.Args[i], stmt.Args[i+2]

			var stripPrefix uint64

			if i+4 < len(stmt.Args) && stmt.Args[i+3].string() == "STRIP_PREFIX" {
				var err error
				stripPrefix, err = strconv.ParseUint(strings.TrimPrefix(stmt.Args[i+4].string(), "+"), 10, 32)

				if err != nil {
					throwFmt("gen: %s: STRIP_PREFIX must be integral in DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args %v", modulePath, stmt.Args)
				}

				i += 2
			}

			bundle[platform.string()] = ResourceBundleEntry{URI: uri.string(), StripPrefix: uint32(stripPrefix)}
		}

		if i != len(stmt.Args) {
			throwFmt("gen: %s: malformed DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args %v", modulePath, stmt.Args)
		}

		return []ResourceDecl{makeHostResourceDecl(stmt.Args[0].string(), bundle)}
	}

	name := stmt.Args[0]
	bundle := readResourceBundleJSON(fs, moduleSourceRel(modulePath, stmt.Args[1].string()))

	return []ResourceDecl{makeHostResourceDecl(name.string(), bundle)}
}

func makeHostResourceDecl(name string, bundle map[string]ResourceBundleEntry) ResourceDecl {
	if len(bundle) == 1 {
		for platform, resource := range bundle {
			if platform == "ANY" {
				return makeResourceDescriptorDecl(name, ResourceDescriptor{URI: resource.URI, StripPrefix: resource.StripPrefix})
			}
		}
	}

	keys := make([]string, 0, len(bundle))

	for k := range bundle {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	resources := make([]ResourceDescriptor, 0, len(keys))

	for _, key := range keys {
		resource := bundle[key]

		resources = append(resources, ResourceDescriptor{Platform: key, URI: resource.URI, StripPrefix: resource.StripPrefix})
	}

	return makeResourceDescriptorDecl(name, ResourceDescriptor{Resources: resources})
}

func resourceDescriptorPlatform(platform string) string {
	parts := strings.Split(strings.ToLower(platform), "-")

	if len(parts) == 3 {
		parts = parts[1:]
	}

	return canonizeResourcePlatform(strings.Join(parts, "-"))
}

func selectResourceDescriptor(descriptor ResourceDescriptor, hostPlatform string) ResourceDescriptor {
	if descriptor.Resources == nil {
		return descriptor
	}

	want := resourceDescriptorPlatform(hostPlatform)

	for _, resource := range descriptor.Resources {
		if strings.ToLower(resource.Platform) == want {
			return resource
		}
	}

	panic(exceptionf("fetch: resource %q has no entry for host platform %q", descriptor.Pattern, hostPlatform))
}

func sortedResourceGlobals(in []ResourceDecl) []ResourceDecl {
	out := append([]ResourceDecl(nil), in...)

	slices.SortFunc(out, func(a, b ResourceDecl) int {
		return strings.Compare(a.GlobalVar.string(), b.GlobalVar.string())
	})

	return slices.Compact(out)
}

func resolveResourceGlobalRef(s string, globals []ResourceDecl) string {
	name, ok := strings.CutPrefix(s, "$")

	if !ok {
		return s
	}

	name = strings.TrimPrefix(strings.TrimSuffix(name, "}"), "{")

	for _, d := range globals {
		if d.GlobalVar.string() == name {
			return d.Value.string()
		}
	}

	panic(exceptionf("resources: %q references resource global not in the PEERDIR closure", s))
}

func resolveResourceGlobalArgs(args []ANY, globals []ResourceDecl) []ANY {
	return resourceGlobalArgumentValue(args, globals).Args
}

func resourceGlobalArgumentValue(args []ANY, globals []ResourceDecl) CompilerFlagValue {
	var uses []STR
	var env Environment
	var out []ANY

	for i, a := range args {
		s := a.string()

		if !strings.Contains(s, resourceGlobalSuffix) {
			if out != nil {
				out = append(out, a)
			}

			continue
		}

		if env.s == nil {
			env = newEnvironment()
			env.resourceUses = &uses

			for _, d := range globals {
				env.setResource(d)
			}
		}

		if out == nil {
			out = append(make([]ANY, 0, len(args)), args[:i]...)
		}

		out = append(out, internAny(expandStmtToken(s, env)))
	}

	if out == nil {
		return CompilerFlagValue{Args: args}
	}

	return CompilerFlagValue{Args: out, Resources: uses}
}

func (e *EmitContext) bindResourceGlobalVars(env Environment) bool {
	ctx, instance, d := e.ctx, e.instance, e.d
	bound := false

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.relString(), stmt) {
			env.setResource(decl)
			bound = true
		}
	}

	return bound
}

type ModuleToolchain struct {
	ClangResource STR
	ClangRoot     VFS
	CC            VFS
	CXX           VFS
	AR            VFS
	Objcopy       VFS
	Strip         VFS
	LLDRoot       VFS
	ARCmdHead     []ANY
	LLD           VFS
	Python3       VFS
}

func isClangToolResourceName(name, clangVer string) bool {
	return len(name) == len(resourcePatternClangTool)+len(clangVer) &&
		strings.HasPrefix(name, resourcePatternClangTool) &&
		name[len(resourcePatternClangTool):] == clangVer
}

type ToolchainKey struct {
	clangVer string
	bits     uint8
}

func resolveModuleToolchain(ctx *GenCtx, globals []ResourceDecl, clangVer string) ModuleToolchain {
	key := ToolchainKey{clangVer: clangVer}

	for _, decl := range globals {
		switch {
		case isClangToolResourceName(decl.Name.string(), clangVer):
			key.bits |= 1
		case decl.Name == strLLDRootName:
			key.bits |= 2
		case decl.Name == strYMakePython3Name:
			key.bits |= 4
		}
	}

	if tc, ok := ctx.tcMemo[key]; ok {
		return tc
	}

	clangRes := resourcePatternClangTool + clangVer
	clangResID := internStr(clangRes)

	var tc ModuleToolchain

	for _, decl := range globals {
		switch decl.Name {
		case clangResID:
			const pfx = "resources/"

			tc.ClangResource = clangResID
			tc.ClangRoot = build(pfx, clangRes)
			tc.CC = build(pfx, clangRes, "/bin/clang")
			tc.CXX = build(pfx, clangRes, "/bin/clang++")
			tc.AR = build(pfx, clangRes, "/bin/llvm-ar")
			tc.Objcopy = build(pfx, clangRes, "/bin/llvm-objcopy")
			tc.Strip = build(pfx, clangRes, "/bin/llvm-strip")
		case strLLDRootName:
			const pfx = "resources/"

			tc.LLDRoot = build(pfx, resourcePatternLLDRoot)
			tc.LLD = build(pfx, resourcePatternLLDRoot, "/bin/ld.lld")
		case strYMakePython3Name:
			tc.Python3 = build("resources/", resourcePatternYMakePython3, "/bin/python3")
		}
	}

	tc.ARCmdHead = []ANY{
		tc.Python3.any(),
		buildScriptsLinkLibPy.any(),
		tc.AR.any(),
		internStr(arTypeLLVM).any(),
		internStr(arFormatGNU).any(),
		argB.any(),
		argNone.any(),
		arg2.any(),
	}

	ctx.tcMemo[key] = tc

	if ownershipOn {
		registerOwnedSlice(tc.ARCmdHead)
	}

	return tc
}

func (e *EmitContext) resourceDeclarations() []ResourceDecl {
	var declarations []ResourceDecl

	positions := make(map[STR]int)

	for _, bundles := range []bool{false, true} {
		for _, stmt := range e.d.resourceDeclStmts {
			if (stmt.Macro != tokDeclareExternalResource) != bundles {
				continue
			}

			for _, decl := range resolveResourceDecls(e.ctx.fs, e.ctx.host, e.instance.Path.relString(), stmt) {
				if position, exists := positions[decl.Name]; exists {
					declarations[position] = decl
				} else {
					positions[decl.Name] = len(declarations)
					declarations = append(declarations, decl)
				}
			}
		}
	}

	return declarations
}

func (e *EmitContext) genResourcesLibrary() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	globals := e.resourceDeclarations()

	for _, decl := range globals {
		emitResourceFetch(ctx, decl)
	}

	closure := e.resourcesLibraryPeerClosure(globals)

	e.frame.peerCtx = PeerContext{ResourceGlobals: closure.resourceGlobals}
	e.peers = &e.frame.peerCtx

	d.tc = resolveModuleToolchain(ctx, closure.resourceGlobals, instance.Platform.ClangVer)
	d.cc.TC = d.tc
	e.bindCompilerFlagResources(closure.resourceGlobals)
	e.emit()

	_, own := e.partitionCollected()

	for _, out := range own.outs {
		e.codegen.use(out)
	}

	globalRefs := ctx.na.refList(append(closure.globalRefs, own.refs...)...)
	globalPaths := ctx.na.vfsList(append(closure.globalPaths, own.outs...)...)

	var sbomRef *NodeRef
	var sbomPath *VFS

	if sbomActive(ctx, instance) && d.toolchainName != "" {
		if instance.Path.relString() != pythonToolchainInfoRel {
			pythonToolchainSbomComponent(ctx, instance.Platform)
		}

		sbomRef, sbomPath = e.emitSbomToolchainComponent(d.toolchainName, d.modver)
	}

	result := &ModuleEmitResult{
		ModuleStmtName:          d.moduleStmt.Name,
		ResourceGlobalClosure:   closure.resourceGlobals,
		PeerArchiveClosureRefs:  closure.archiveRefs,
		PeerArchiveClosurePaths: closure.archivePaths,
		PeerGlobalClosureRefs:   globalRefs,
		PeerGlobalClosurePaths:  globalPaths,
		PeerSbomClosureRefs:     closure.sbomRefs,
		PeerSbomClosurePaths:    closure.sbomPaths,
		RPathFlagsGlobal:        d.rpathFlagsGlobal,
		LDFlagsGlobal:           e.ownLinkerFlagValues(d.ldFlags),
		GoLDFlagsGlobal:         e.ownLinkerFlagValues(d.goLdflags),
		CFlagsGlobal:            e.ownCompilerFlagValues(d.cFlagsGlobal, compilerFlagsCommon),
		CXXFlagsGlobal:          e.ownCompilerFlagValues(d.cxxFlagsGlobal, compilerFlagsCxx),
		COnlyFlagsGlobal:        e.ownCompilerFlagValues(d.cOnlyFlagsGlobal, compilerFlagsCOnly),
		LinkExcludeLibraries:    d.linkExcludeLibraries,
		ObjAddLibsGlobal:        d.objAddLibsGlobal,
		AddInclGlobal:           closure.addInclGlobal,
		OwnAddInclGlobal:        d.addInclGlobal,
		AddInclUserGlobal:       d.addInclUserGlobal,
		AddInclOneLevel:         d.addInclOneLevel,
		SbomComponentRef:        sbomRef,
		SbomComponentPath:       sbomPath,
	}

	ctx.memoPut(instance, result)

	return result
}

type ResourcesLibraryPeerClosure struct {
	resourceGlobals []ResourceDecl
	archiveRefs     []NodeRef
	archivePaths    []VFS
	globalRefs      []NodeRef
	globalPaths     []VFS
	sbomRefs        []NodeRef
	sbomPaths       []VFS
	addInclGlobal   []VFS
}

func (e *EmitContext) resourcesLibraryPeerClosure(own []ResourceDecl) ResourcesLibraryPeerClosure {
	c := ResourcesLibraryPeerClosure{resourceGlobals: slices.Clip(own), addInclGlobal: e.d.addInclGlobal}

	if len(e.d.peerdirs) == 0 {
		return c
	}

	peers := make([]ResolvedPeer, 0, len(e.d.peerdirs))

	for _, peer := range e.d.peerdirs {
		path := peerdirModulePath(peer).string()

		peers = append(peers, ResolvedPeer{path: path, result: e.genPeerModule(e.derivePeerInstance(path)), kind: peerKindUserPeer})
	}

	dedupers.with(func(dd *DeDuper) {
		for _, decl := range own {
			dd.addStable(decl.GlobalVar.strID())
		}

		for _, rp := range peers {
			for _, decl := range rp.result.ResourceGlobalClosure {
				if dd.addStable(decl.GlobalVar.strID()) {
					c.resourceGlobals = append(c.resourceGlobals, decl)
				}
			}
		}

		dd.reset()

		for _, rp := range peers {
			pr := rp.result

			for i, p := range pr.PeerArchiveClosurePaths {
				if dd.addStable(p.strID()) {
					c.archiveRefs = append(c.archiveRefs, pr.PeerArchiveClosureRefs[i])
					c.archivePaths = append(c.archivePaths, p)
				}
			}

			if pr.ARPath != nil && dd.addStable(pr.ARPath.strID()) {
				c.archiveRefs = append(c.archiveRefs, pr.ARRef)
				c.archivePaths = append(c.archivePaths, *pr.ARPath)
			}
		}

		dd.reset()

		for _, rp := range peers {
			pr := rp.result

			for i, p := range pr.PeerGlobalClosurePaths {
				if dd.addStable(p.strID()) {
					c.globalRefs = append(c.globalRefs, pr.PeerGlobalClosureRefs[i])
					c.globalPaths = append(c.globalPaths, p)
				}
			}

			if pr.GlobalRef != nil && pr.GlobalPath != nil && dd.addStable(pr.GlobalPath.strID()) {
				c.globalRefs = append(c.globalRefs, *pr.GlobalRef)
				c.globalPaths = append(c.globalPaths, *pr.GlobalPath)
			}
		}

		if e.ctx.sbomEnabled {
			c.sbomRefs, c.sbomPaths, _ = aggregateSbomComponents(dd, e, e.d.moduleStmt.Name, false, peers, nil, nil, nil)
		}

		dd.reset()

		var peerAddIncl []VFS
		var oneLevelOnly map[VFS]struct{}

		for _, rp := range peers {
			peerAddIncl, oneLevelOnly = appendUserPeerAddIncl(dd, peerAddIncl, oneLevelOnly, rp.result)
		}

		dd.reset()
		c.addInclGlobal = slices.Clip(e.d.addInclGlobal)

		for _, p := range c.addInclGlobal {
			dd.addStable(p.strID())
		}

		for _, p := range peerAddIncl {
			if _, isOneLevel := oneLevelOnly[p]; !isOneLevel && dd.addStable(p.strID()) {
				c.addInclGlobal = append(c.addInclGlobal, p)
			}
		}
	})

	return c
}

func prebuiltModuleSuffix(p *Platform) string {
	if p.OS == OSWindows {
		return ".exe"
	}

	return ""
}

func (e *EmitContext) genPrebuiltProgram() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	globals := e.resourceDeclarations()
	primary := Environment{s: &e.frame.ifEnv}.expand(d.primaryOutput)

	var resourceRefs []NodeRef

	for _, decl := range globals {
		if primary == decl.Value.string() || strings.HasPrefix(primary, decl.Value.string()+"/") {
			resourceRefs = append(resourceRefs, emitResourceFetch(ctx, decl))

			break
		}
	}

	dst := lDOutputPath(instance, programBinaryName(instance, d.moduleStmt))
	env := envVarsVCS

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) {
		ownSbomRef, ownSbomPath = e.emitSbomComponent(programBinaryName(instance, d.moduleStmt))
	}

	pyRef, pyPath := (*NodeRef)(nil), (*VFS)(nil)

	if sbomActive(ctx, instance) && instance.Platform.BuildRelease {
		pyRef, pyPath = pythonToolchainSbomComponent(ctx, instance.Platform)
	}

	inputs := na.inputs.alloc(4)[:0]
	depRefs := na.noderefs.alloc(3)[:0]

	inputs = append(inputs, ctx.scripts[copyFsToolsVFS.rel()])
	depRefs = append(depRefs, resourceRefs...)

	if pyRef != nil {
		inputs = append(inputs, na.vfsList(*pyPath))
		depRefs = append(depRefs, *pyRef)
	}

	if ownSbomRef != nil && instance.Platform.BuildRelease {
		inputs = append(inputs, na.vfsList(*ownSbomPath))
		depRefs = append(depRefs, *ownSbomRef)
	}

	na.inputs.commit(len(inputs))
	na.noderefs.commit(len(depRefs))

	inputsChunks := InputChunks(inputs[:len(inputs):len(inputs)])

	depRefs = depRefs[:len(depRefs):len(depRefs)]

	args := na.anys.alloc(5)[:0]

	args = append(args, wrapccPython3STR.any(), copyFsToolsVFS.any(), argCopy.any())

	if primary != "" {
		args = append(args, internAny(primary))
	}

	args = append(args, dst.any())
	na.anys.commit(len(args))

	node := Node{
		Platform: instance.Platform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(args[:len(args):len(args)]), Env: env}),
		Env:      env, Inputs: inputsChunks, KV: &resourcesKV,
		Outputs:   na.vfsList(dst),
		DepRefs:   depRefs,
		Resources: usesPython3,
	}

	ref := e.emitNode(NodeSpec{Node: node})

	result := &ModuleEmitResult{
		ModuleStmtName: d.moduleStmt.Name,
		ARRef:          ref,
		ARPath:         &dst,
		isPROGRAM:      true,
		LDRef:          ref,
		LDPath:         &dst,

		InducedDeps: d.inducedDeps,
	}

	ctx.memoPut(instance, result)

	return result
}

type ResourceBundleJSONStripPrefix struct {
	value float64
	null  bool
}

func (p *ResourceBundleJSONStripPrefix) UnmarshalJSON(data []byte) error {
	return p.unmarshalJSON(data)
}

func (p *ResourceBundleJSONStripPrefix) unmarshalJSON(data []byte) error {
	*p = ResourceBundleJSONStripPrefix{}

	if bytes.Equal(data, []byte("null")) {
		p.null = true

		return nil
	}

	return json.Unmarshal(data, &p.value)
}

type ResourceBundleJSONEntry struct {
	URI         *string                       `json:"uri"`
	StripPrefix ResourceBundleJSONStripPrefix `json:"strip_prefix"`
}

type ResourceBundleJSON struct {
	ByPlatform NativeJSONMap[ResourceBundleJSONEntry] `json:"by_platform"`
}

func moduleSourceRel(modulePath, arg string) string {
	if suffix, ok := strings.CutPrefix(arg, "$(S)/"); ok {
		return cleanRel(suffix)
	}

	return cleanRel(joinRel(modulePath, arg))
}

func readResourceBundleJSON(fs FS, rel string) map[string]ResourceBundleEntry {
	key := internStr(rel)

	if bundle, ok := fs.resourceBundle(key); ok {
		return bundle
	}

	bundle := parseResourceBundleJSON(concatChunks(fs.readPath(key)))

	fs.storeResourceBundle(key, bundle)

	return bundle
}

func parseResourceBundleJSON(text []byte) map[string]ResourceBundleEntry {
	var data ResourceBundleJSON
	throw(json.Unmarshal(text, &data))

	out := make(map[string]ResourceBundleEntry, len(data.ByPlatform.members))

	data.ByPlatform.each(func(platform string, wire ResourceBundleJSONEntry) {
		if wire.URI == nil {
			throwFmt("resources: platform %q: uri is not found or its value is not a string", platform)
		}

		prefix := wire.StripPrefix.value

		if wire.StripPrefix.null || prefix < 0 || math.Trunc(prefix) != prefix {
			throwFmt("resources: platform %q: strip_prefix value should be integral", platform)
		}

		if prefix > 65535 {
			throwFmt("resources: platform %q: strip_prefix value %s is too big", platform, strconv.FormatFloat(prefix, 'f', -1, 64))
		}

		resource := ResourceBundleEntry{URI: *wire.URI, StripPrefix: uint32(prefix)}
		key := strings.ToUpper(canonizeResourcePlatform(platform))

		if _, exists := out[key]; !exists {
			out[key] = resource
		}
	})

	if len(out) == 0 {
		throwFmt("resources: platform mapping is empty")
	}

	return out
}

func resourceGlobalTokenValue(args []ANY, globals []ResourceDecl) CompilerFlagValue {
	if len(globals) == 0 {
		return CompilerFlagValue{Args: args}
	}

	for _, arg := range args {
		if !strHasDollar(arg) {
			continue
		}

		env := newEnvironment()

		var uses []STR
		env.resourceUses = &uses

		for _, decl := range globals {
			env.setResource(decl)
		}

		return CompilerFlagValue{Args: expandStmtTokens(args, env), Resources: uses}
	}

	return CompilerFlagValue{Args: args}
}
