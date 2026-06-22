package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

var (
	strLLDRootName      = internStr(resourcePatternLLDRoot)
	strYMakePython3Name = internStr(resourcePatternYMakePython3)
	// Shared Resources vectors referenced by every node needing the fixed set.
	usesPython3        = []STR{strYMakePython3Name}
	usesPython3JDK17   = []STR{strYMakePython3Name, internStr(resourcePatternJDK17)}
	usesPython3Clang16 = []STR{strYMakePython3Name, internStr(resourcePatternClang16)}
)

// External-resource model. A RESOURCES_LIBRARY declares external resources via
// DECLARE_*. Each yields a <Name>_RESOURCE_GLOBAL variable (the bare "$(<Name>)"
// ref, propagated through the PEERDIR closure) and a fetch of the host-selected
// URI into the $(<Name>) resource dir. Every field is interned.

// resourceDecl is one declared external resource after host-platform selection.
type ResourceDecl struct {
	Name      STR // resource base name
	URI       STR // host-selected uri ("sbr:<id>" or an absolute path)
	GlobalVar STR // propagated variable name (<Name>_RESOURCE_GLOBAL)
	Value     STR // variable value: the bare resource ref "$(<Name>)"
	Token     STR // --global-resource arg "<Name>_RESOURCE_GLOBAL::$(<Name>)"
}

const resourceGlobalSuffix = "_RESOURCE_GLOBAL"

// platformDefaultArch is the implicit arch in a by_platform key: "linux-x86_64"
// canonizes to "linux".
const platformDefaultArch = "x86_64"

// makeResourceDecl interns one resource: the bare ref, global-var name and
// --global-resource token. The uri drives the fetch.
func makeResourceDecl(name, uri string) ResourceDecl {
	// Resource references resolve to the FETCH node's output dir $(B)/resources/NAME,
	// so ${NAME_RESOURCE_GLOBAL} points at a real graph output the consumer depends
	// on. dump normalize folds it back to $(NAME).
	value := "$(B)/resources/" + name
	globalVar := name + resourceGlobalSuffix

	return ResourceDecl{
		Name:      internStr(name),
		URI:       internStr(uri),
		GlobalVar: internStr(globalVar),
		Value:     internStr(value),
		Token:     internStr(globalVar + "::" + value),
	}
}

// hostPlatformKey is the by_platform json key for the host (os-isa). Resource
// bundles select the HOST entry; these are host tools.
func hostPlatformKey(host *Platform) string {
	return string(host.OS) + "-" + isaPlatformKey(host.ISA)
}

// resourceJSONPlatformKey is the by_platform key for the instance's platform,
// canonized with x86_64 implicit and win as "win32".
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

// canonizePlatformKey lower-cases a by_platform key to <os>[-<arch>] with the
// default arch dropped, so "linux" and "linux-x86_64" both collapse to "linux".
func canonizePlatformKey(key string) string {
	key = strings.ToLower(key)

	os, arch, found := strings.Cut(key, "-")

	if !found || arch == "" || arch == platformDefaultArch {
		return os
	}

	return os + "-" + arch
}

// resolveResourceURIFromBundle returns the bundle URI for env's platform,
// matching keys by canonical form, scanned in sorted order for determinism.
func resolveResourceURIFromBundle(bundle map[string]string, env Environment) (string, bool) {
	want := resourceJSONPlatformKey(env)

	keys := make([]string, 0, len(bundle))

	for k := range bundle {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		if canonizePlatformKey(k) == want {
			return bundle[k], true
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

// stripSbrPrefix returns the bare sandbox id of an "sbr:<id>" uri.
func stripSbrPrefix(uri string) string {
	return strings.TrimPrefix(uri, "sbr:")
}

// resolveResourceDecls turns one DECLARE_* call into host-selected declarations.
func resolveResourceDecls(fs FS, host *Platform, modulePath string, stmt *DeclareResourceStmt) []ResourceDecl {
	switch stmt.Macro {
	case tokDeclareExternalResource:
		// NAME uri [NAME2 uri2 ...] — direct, host-independent.
		out := make([]ResourceDecl, 0, len(stmt.Args)/2)

		for i := 0; i+1 < len(stmt.Args); i += 2 {
			out = append(out, makeResourceDecl(stmt.Args[i].string(), stmt.Args[i+1].string()))
		}

		return out
	case tokDeclareExternalHostResourcesBundle:
		// NAME uri FOR platform ... — select the host entry.
		name := stmt.Args[0]
		bundle := map[string]string{}

		for i := 1; i+2 < len(stmt.Args); i += 3 {
			if stmt.Args[i+1].string() != "FOR" {
				throwFmt("gen: %s: malformed DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args %v", modulePath, stmt.Args)
			}

			bundle[stmt.Args[i+2].string()] = stmt.Args[i].string()
		}

		return []ResourceDecl{selectHostResourceDecl(host, modulePath, name.string(), bundle)}
	case tokDeclareExternalHostResourcesBundleByJson:
		// NAME file.json — read by_platform.<host>.uri.
		name, jsonRel := stmt.Args[0], stmt.Args[1]
		bundle := readResourceBundleJSON(fs, filepath.ToSlash(filepath.Join(modulePath, jsonRel.string())))

		return []ResourceDecl{selectHostResourceDecl(host, modulePath, name.string(), bundle)}
	}

	return nil
}

func selectHostResourceDecl(host *Platform, modulePath, name string, bundle map[string]string) ResourceDecl {
	uri, ok := bundle[hostPlatformKey(host)]

	if !ok {
		throwFmt("gen: %s: resource %q has no entry for host platform %q", modulePath, name, hostPlatformKey(host))
	}

	return makeResourceDecl(name, uri)
}

// sortedResourceGlobals orders declarations by global-var name — the order a test
// node's --global-resource args are emitted.
func sortedResourceGlobals(in []ResourceDecl) []ResourceDecl {
	out := append([]ResourceDecl(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].GlobalVar.string() < out[j].GlobalVar.string()
	})

	return out
}

// resolveResourceGlobalRef expands a deferred $<NAME>_RESOURCE_GLOBAL reference
// against the consuming module's resource-global closure, resolving to the decl's
// value; a non-reference passes through. Deferred until command generation.
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

	throwFmt("resources: %q references resource global not in the PEERDIR closure", s)

	return ""
}

// bindResourceGlobalVars resolves a RESOURCES_LIBRARY's DECLARE_* statements and
// binds each <NAME>_RESOURCE_GLOBAL env var. Returns whether any var was bound, so
// the caller re-collects to expand references that precede the DECLARE.
func bindResourceGlobalVars(ctx *GenCtx, instance ModuleInstance, d *ModuleData, env Environment) bool {
	bound := false

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.rel(), stmt) {
			env.setStringID(internEnvSTR(decl.GlobalVar), decl.Value)
			bound = true
		}
	}

	return bound
}

// moduleToolchain holds a module's tool-invocation paths, derived from the
// external-resource globals reachable through its PEERDIR closure. A field stays 0
// when its resource is absent from the closure (caught at use, never defaulted).
type ModuleToolchain struct {
	// ClangResource is the versioned clang resource the compiler/llvm tools come
	// from, version-specific so several clang versions can coexist. Consumers list
	// it in their Resources to depend on that FETCH node.
	ClangResource STR
	ClangRoot     STR
	CC            STR
	CXX           STR
	AR            STR
	Objcopy       STR
	Strip         STR
	LLDRoot       STR

	// ARCmdHead is the pre-built head of every AR command this toolchain drives,
	// referenced as a chunk by emitARNode, never copied.
	ARCmdHead []STR
	LLD       STR
	Python3   STR
}

func resolveModuleToolchain(globals []ResourceDecl, clangVer string) ModuleToolchain {
	var tc ModuleToolchain

	// The compiler/llvm tools come from the version-specific clang resource.
	clangRes := resourcePatternClangTool + clangVer
	// Decl names are compared in id space: one intern probe per call.
	clangResID := internStr(clangRes)

	for _, decl := range globals {
		switch decl.Name {
		case clangResID:
			root := "$(B)/resources/" + clangRes
			tc.ClangResource = clangResID
			tc.ClangRoot = internStr(root)
			tc.CC = internStr(root + "/bin/clang")
			tc.CXX = internStr(root + "/bin/clang++")
			tc.AR = internStr(root + "/bin/llvm-ar")
			tc.Objcopy = internStr(root + "/bin/llvm-objcopy")
			tc.Strip = internStr(root + "/bin/llvm-strip")
		case strLLDRootName:
			root := "$(B)/resources/" + resourcePatternLLDRoot
			tc.LLDRoot = internStr(root)
			tc.LLD = internStr(root + "/bin/ld.lld")
		case strYMakePython3Name:
			tc.Python3 = internStr("$(B)/resources/" + resourcePatternYMakePython3 + "/bin/python3")
		}
	}

	tc.ARCmdHead = []STR{
		tc.Python3,
		(buildScriptsLinkLibPy).str(),
		tc.AR,
		internStr(arTypeLLVM),
		internStr(arFormatGNU),
		argB.str(),
		argNone.str(),
		arg2.str(),
	}

	return tc
}

// genResourcesLibrary emits a RESOURCES_LIBRARY: no archive/objects, only the
// external resource globals it declares, which propagate up the PEERDIR closure.
func genResourcesLibrary(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	var globals []ResourceDecl
	deduper.reset()

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.rel(), stmt) {
			if deduper.add(VFS(decl.GlobalVar)) {
				globals = append(globals, decl)
				emitResourceFetch(ctx, decl)
			}
		}
	}

	// A RESOURCES_LIBRARY has no PEERDIRs, so its GLOBAL contributions propagate as
	// the general path would with an empty peer set — how LDFLAGS_GLOBAL and
	// RPATH_GLOBAL reach every linking consumer.
	var sbomRef *NodeRef
	var sbomPath *VFS

	if sbomActive(ctx, instance) && d.toolchainName != "" {
		// The toolchain SBOM node runs the python resource; this path resolves no
		// peers, so resolve the universal python peer explicitly to register the
		// python FETCH into ctx.fetchRefs BEFORE this node. The python toolchain
		// declares the python resource itself, so skip it.
		if instance.Path.rel() != pythonToolchainInfoRel {
			pythonToolchainSbomComponent(ctx, instance.Platform)
		}

		sbomRef, sbomPath = emitSbomToolchainComponent(ctx, instance, d.toolchainName, d.modver)
	}

	result := &ModuleEmitResult{
		ModuleStmtName:        d.moduleStmt.Name,
		ResourceGlobalClosure: globals,
		Peerdirs:              d.peerdirs,
		RPathFlagsGlobal:      dedupARG(d.rpathFlagsGlobal),
		LDFlagsGlobal:         dedupARG(d.ldFlags),
		CFlagsGlobal:          dedupARG(d.cFlagsGlobal),
		CXXFlagsGlobal:        dedupARG(d.cxxFlagsGlobal),
		COnlyFlagsGlobal:      dedupARG(d.cOnlyFlagsGlobal),
		ObjAddLibsGlobal:      dedupARG(d.objAddLibsGlobal),
		AddInclGlobal:         dedupVFS(d.addInclGlobal, nil),
		OwnAddInclGlobal:      d.addInclGlobal,
		AddInclUserGlobal:     d.addInclUserGlobal,
		AddInclOneLevel:       d.addInclOneLevel,
		SbomComponentRef:      sbomRef,
		SbomComponentPath:     sbomPath,
	}
	ctx.memo.put(ctx.instanceKey(instance), result)

	return result
}

// prebuiltModuleSuffix is the PROGRAM MODULE_SUFFIX for the platform (empty, .exe
// under WIN32), spliced after the binary name by PREBUILT_PROGRAM's PRIMARY_OUTPUT.
func prebuiltModuleSuffix(p *Platform) string {
	if p.OS == OSWindows {
		return ".exe"
	}

	return ""
}

// genPrebuiltProgram emits a PREBUILT_PROGRAM: it fetches a sandbox-built binary
// and copies it to the module's program output, so a tool's from-source closure
// never enters the graph. The result is a PROGRAM so tool consumers take it.
func genPrebuiltProgram(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	na := ctx.na

	var fetchRef NodeRef
	var globals []ResourceDecl
	deduper.reset()

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.rel(), stmt) {
			if deduper.add(VFS(decl.Name)) {
				globals = append(globals, decl)
				fetchRef = emitResourceFetch(ctx, decl)
			}
		}
	}

	if d.primaryOutput == "" || len(globals) == 0 {
		throwFmt("gen: %s: PREBUILT_PROGRAM has no PRIMARY_OUTPUT/resource", instance.Path.rel())
	}

	if strings.Contains(d.primaryOutput, "${") {
		throwFmt("gen: %s: PREBUILT_PROGRAM PRIMARY_OUTPUT %q has an unresolved reference", instance.Path.rel(), d.primaryOutput)
	}

	// primaryOutput is "$(B)/resources/<NAME>/<bin>" (the fetch node's output dir);
	// the copy reads it and depends on the fetch. dst is the module's program output.
	srcVFS := build(strings.TrimPrefix(d.primaryOutput, "$(B)/"))
	dst := lDOutputPath(instance, programBinaryName(instance, d.moduleStmt))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// The prebuilt copy gets a _GEN_SBOM_COMPONENT; on a RELEASE host build with
	// EMBED the copy node collects its own .component.sbom.
	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) {
		ownSbomRef, ownSbomPath = emitSbomComponent(ctx, instance, d, programBinaryName(instance, d.moduleStmt))
	}

	// The copy command does NOT wrap the primary output in ${input}, so the fetched
	// binary is a resource (tracked via fetchRef), not a content input.
	inputs := InputChunks{ctx.scripts[copyFsToolsVFS]}
	depRefs := []NodeRef{fetchRef}

	// A bare-link PREBUILT_PROGRAM resolves no SBOM peers but the universal python
	// toolchain component _BARE_UNIT adds, so that is its whole peer SBOM closure.
	if sbomActive(ctx, instance) && instance.Platform.BuildRelease {
		if pyRef, pyPath := pythonToolchainSbomComponent(ctx, instance.Platform); pyRef != nil {
			inputs = append(inputs, []VFS{*pyPath})
			depRefs = append(depRefs, *pyRef)
		}
	}

	if ownSbomRef != nil && instance.Platform.BuildRelease {
		// _GEN_SBOM_COMPONENT declares ${input:gen_sbom.py}; that module input lands
		// on the single copy node alongside its own .component.sbom global.
		inputs = append(inputs, []VFS{*ownSbomPath, source(sbomGenScriptRel)})
		depRefs = append(depRefs, *ownSbomRef)
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList([]STR{
			wrapccPython3STR,
			copyFsToolsVFS.str(),
			argCopy.str(),
			srcVFS.str(),
			dst.str(),
		}), Env: env}),
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkld, PC: pcLightBlue, ShowOut: true},
		Outputs:          na.vfsList(dst),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleLang: mlAgnostic, ModuleType: mtBin},
		DepRefs:          depRefs,
		Resources:        usesPython3,
	}

	ref := ctx.emit.emit(node)

	result := &ModuleEmitResult{
		ModuleStmtName: d.moduleStmt.Name,
		ARRef:          ref,
		ARPath:         &dst,
		isPROGRAM:      true,
		LDRef:          ref,
		LDPath:         &dst,
		Peerdirs:       d.peerdirs,
		// A prebuilt codegen tool keeps its INDUCED_DEPS so a generated file's
		// consumer pulls them via GeneratorRefs.
		InducedDeps: d.inducedDeps,
	}
	ctx.memo.put(ctx.instanceKey(instance), result)

	return result
}

type ResourceBundleJSON struct {
	ByPlatform map[string]struct {
		URI string `json:"uri"`
	} `json:"by_platform"`
}

// readResourceBundleJSON parses a bundle json into a platform-key -> uri map.
func readResourceBundleJSON(fs FS, rel string) map[string]string {
	var data ResourceBundleJSON
	throw(json.Unmarshal(fs.read(rel), &data))

	out := make(map[string]string, len(data.ByPlatform))

	for k, v := range data.ByPlatform {
		out[k] = v.URI
	}

	return out
}
